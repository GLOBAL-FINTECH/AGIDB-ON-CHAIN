package enterprise

import (
	"archive/zip"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/global-fintech/agidb/internal/engine"
	"github.com/global-fintech/agidb/internal/storage"
)

type Policy struct {
	Version int `json:"version"`
	Mode string `json:"mode"`
	MaxValueBytes int `json:"max_value_bytes"`
	MaxBatchOperations int `json:"max_batch_operations"`
	MaxLogResults int `json:"max_log_results"`
	SnapshotRetention int `json:"snapshot_retention"`
	BackupRetention int `json:"backup_retention"`
	RequireEncryption bool `json:"require_encryption"`
	AllowedRoles []string `json:"allowed_roles"`
	UpdatedAt time.Time `json:"updated_at"`
}
func DefaultPolicy() Policy{return Policy{Version:1,Mode:"archive",MaxValueBytes:16<<20,MaxBatchOperations:50000,MaxLogResults:10000,SnapshotRetention:8,BackupRetention:8,AllowedRoles:[]string{"reader","writer","executor","auditor","snapshot-manager","administrator"},UpdatedAt:time.Now().UTC()}}
func WritePolicy(dir string,p Policy)(string,error){p.UpdatedAt=time.Now().UTC();b,err:=json.MarshalIndent(p,"","  ");if err!=nil{return "",err};path:=filepath.Join(dir,"meta","policy.json");if err:=os.MkdirAll(filepath.Dir(path),0o700);err!=nil{return "",err};return path,atomicWrite(path,b,0o600)}
func ReadPolicy(dir string)(Policy,error){b,err:=os.ReadFile(filepath.Join(dir,"meta","policy.json"));if errors.Is(err,os.ErrNotExist){return DefaultPolicy(),nil};var p Policy;if err==nil{err=json.Unmarshal(b,&p)};return p,err}

type ClusterConfig struct{Version int `json:"version"`;ClusterID string `json:"cluster_id"`;NodeID string `json:"node_id"`;Peers []string `json:"peers"`;Term uint64 `json:"term"`;Leader string `json:"leader"`;CommitIndex uint64 `json:"commit_index"`;Quorum int `json:"quorum"`;Mode string `json:"mode"`;UpdatedAt time.Time `json:"updated_at"`}
func InitCluster(dir,nodeID string,peers []string)(ClusterConfig,string,error){if nodeID==""{return ClusterConfig{},"",errors.New("node id required")};all:=append([]string{nodeID},peers...);sort.Strings(all);h:=sha256.Sum256([]byte(strings.Join(all,"\x00")));q:=len(all)/2+1;c:=ClusterConfig{Version:1,ClusterID:hex.EncodeToString(h[:8]),NodeID:nodeID,Peers:peers,Term:1,Quorum:q,Mode:"deterministic-consensus-test-framework",UpdatedAt:time.Now().UTC()};path:=filepath.Join(dir,"cluster","config.json");b,_:=json.MarshalIndent(c,"","  ");return c,path,atomicWrite(path,b,0o600)}
func ReadCluster(dir string)(ClusterConfig,error){b,err:=os.ReadFile(filepath.Join(dir,"cluster","config.json"));if err!=nil{return ClusterConfig{},err};var c ClusterConfig;err=json.Unmarshal(b,&c);return c,err}
func ElectLeader(dir string,candidates []string)(ClusterConfig,error){c,err:=ReadCluster(dir);if err!=nil{return c,err};if len(candidates)==0{candidates=append([]string{c.NodeID},c.Peers...)};sort.Strings(candidates);c.Term++;c.Leader=candidates[0];c.UpdatedAt=time.Now().UTC();b,_:=json.MarshalIndent(c,"","  ");err=atomicWrite(filepath.Join(dir,"cluster","config.json"),b,0o600);return c,err}
type QuorumResult struct{Nodes int `json:"nodes"`;Acks int `json:"acks"`;Required int `json:"required"`;Committed bool `json:"committed"`;Term uint64 `json:"term"`;Leader string `json:"leader"`}
func SimulateQuorum(c ClusterConfig,nodes,acks int)QuorumResult{req:=nodes/2+1;return QuorumResult{Nodes:nodes,Acks:acks,Required:req,Committed:acks>=req,Term:c.Term,Leader:c.Leader}}

func Backup(dir,out,passphrase string)(string,string,error){if out==""{out=filepath.Join(dir,"backups",fmt.Sprintf("agidb-backup-%s.agibak",time.Now().UTC().Format("20060102T150405Z")))};if err:=os.MkdirAll(filepath.Dir(out),0o700);err!=nil{return "","",err};var raw bytes.Buffer;zw:=zip.NewWriter(&raw);err:=filepath.Walk(dir,func(path string,info os.FileInfo,err error)error{if err!=nil{return err};if info.IsDir(){return nil};rel,err:=filepath.Rel(dir,path);if err!=nil{return err};if rel==filepath.Join("meta","LOCK")||strings.HasPrefix(rel,"backups"+string(os.PathSeparator)){return nil};h,err:=zip.FileInfoHeader(info);if err!=nil{return err};h.Name=filepath.ToSlash(rel);h.Method=zip.Deflate;w,err:=zw.CreateHeader(h);if err!=nil{return err};f,err:=os.Open(path);if err!=nil{return err};defer f.Close();_,err=io.Copy(w,f);return err});if err!=nil{return "","",err};if err=zw.Close();err!=nil{return "","",err};payload:=raw.Bytes();if passphrase!=""{enc,err:=encrypt(payload,passphrase);if err!=nil{return "","",err};payload=append([]byte("AGIENC1\n"),enc...)}else{payload=append([]byte("AGIBAK1\n"),payload...)};if err=atomicWrite(out,payload,0o600);err!=nil{return "","",err};h:=sha256.Sum256(payload);return out,hex.EncodeToString(h[:]),nil}
func VerifyBackup(path,passphrase string)(map[string]any,error){payload,err:=os.ReadFile(path);if err!=nil{return nil,err};sum:=sha256.Sum256(payload);raw,err:=decodeBackup(payload,passphrase);if err!=nil{return nil,err};zr,err:=zip.NewReader(bytes.NewReader(raw),int64(len(raw)));if err!=nil{return nil,err};names:=make([]string,0,len(zr.File));for _,f:=range zr.File{names=append(names,f.Name)};sort.Strings(names);return map[string]any{"verified":true,"sha256":hex.EncodeToString(sum[:]),"files":len(names),"entries":names,"encrypted":bytes.HasPrefix(payload,[]byte("AGIENC1\n"))},nil}
func RestoreBackup(path,target,passphrase string)error{if target==""{return errors.New("target directory required")};if _,err:=os.Stat(target);err==nil{entries,_:=os.ReadDir(target);if len(entries)>0{return errors.New("target directory must be empty or absent")}};payload,err:=os.ReadFile(path);if err!=nil{return err};raw,err:=decodeBackup(payload,passphrase);if err!=nil{return err};zr,err:=zip.NewReader(bytes.NewReader(raw),int64(len(raw)));if err!=nil{return err};tmp:=target+".restore.tmp";_ = os.RemoveAll(tmp);if err=os.MkdirAll(tmp,0o700);err!=nil{return err};for _,f:=range zr.File{clean:=filepath.Clean(f.Name);if strings.HasPrefix(clean,"..")||filepath.IsAbs(clean){return errors.New("unsafe backup path")};dst:=filepath.Join(tmp,clean);if err=os.MkdirAll(filepath.Dir(dst),0o700);err!=nil{return err};r,err:=f.Open();if err!=nil{return err};b,err:=io.ReadAll(r);r.Close();if err!=nil{return err};if err=atomicWrite(dst,b,0o600);err!=nil{return err}};return os.Rename(tmp,target)}
func PruneFiles(dir string,snapshotKeep,backupKeep int)(map[string]int,error){out:=map[string]int{"snapshots_removed":0,"backups_removed":0};for sub,cfg:=range map[string]struct{keep int;key string}{"snapshots":{snapshotKeep,"snapshots_removed"},"backups":{backupKeep,"backups_removed"}}{keep,key:=cfg.keep,cfg.key;entries,err:=os.ReadDir(filepath.Join(dir,sub));if errors.Is(err,os.ErrNotExist){continue};if err!=nil{return out,err};type item struct{name string;mod time.Time};var items []item;for _,e:=range entries{if e.IsDir(){continue};info,_:=e.Info();items=append(items,item{e.Name(),info.ModTime()})};sort.Slice(items,func(i,j int)bool{return items[i].mod.After(items[j].mod)});if keep<0{return out,fmt.Errorf("retention count for %s cannot be negative",sub)};if keep>=len(items){continue};for _,it:=range items[keep:]{if err:=os.Remove(filepath.Join(dir,sub,it.name));err==nil{out[key]++}}};return out,nil}
func MigrationReport(dir,target string,status any)(string,error){report:=map[string]any{"source_directory":dir,"target_format":target,"generated_at":time.Now().UTC(),"status":status,"strategy":"copy-verify-atomic-switch","in_place_mutation":false};b,_:=json.MarshalIndent(report,"","  ");path:=filepath.Join(dir,"meta",fmt.Sprintf("migration-%s.json",strings.ToLower(target)));return path,atomicWrite(path,b,0o600)}
func Unlock(dir string)error{return os.Remove(filepath.Join(dir,"meta","LOCK"))}
func atomicWrite(path string,b []byte,mode os.FileMode)error{if err:=os.MkdirAll(filepath.Dir(path),0o700);err!=nil{return err};tmp:=path+".tmp";f,err:=os.OpenFile(tmp,os.O_CREATE|os.O_TRUNC|os.O_WRONLY,mode);if err!=nil{return err};if _,err=f.Write(b);err!=nil{f.Close();return err};if err=f.Sync();err!=nil{f.Close();return err};if err=f.Close();err!=nil{return err};return os.Rename(tmp,path)}
func derive(pass string,salt []byte)[]byte{x:=append([]byte(pass),salt...);h:=sha256.Sum256(x);key:=h[:];for i:=0;i<200000;i++{q:=sha256.Sum256(key);key=q[:]};return append([]byte{},key...)}
func encrypt(p []byte,pass string)([]byte,error){salt:=make([]byte,16);nonce:=make([]byte,12);if _,e:=rand.Read(salt);e!=nil{return nil,e};if _,e:=rand.Read(nonce);e!=nil{return nil,e};block,e:=aes.NewCipher(derive(pass,salt));if e!=nil{return nil,e};g,e:=cipher.NewGCM(block);if e!=nil{return nil,e};ct:=g.Seal(nil,nonce,p,nil);return append(append(salt,nonce...),ct...),nil}
func decrypt(p []byte,pass string)([]byte,error){if len(p)<28{return nil,errors.New("encrypted backup truncated")};salt,nonce,ct:=p[:16],p[16:28],p[28:];block,e:=aes.NewCipher(derive(pass,salt));if e!=nil{return nil,e};g,e:=cipher.NewGCM(block);if e!=nil{return nil,e};return g.Open(nil,nonce,ct,nil)}
func decodeBackup(payload []byte,pass string)([]byte,error){if bytes.HasPrefix(payload,[]byte("AGIBAK1\n")){return payload[8:],nil};if bytes.HasPrefix(payload,[]byte("AGIENC1\n")){if pass==""{return nil,errors.New("backup is encrypted; passphrase required")};return decrypt(payload[8:],pass)};return nil,errors.New("unsupported backup format")}
func Compact(dir string)(map[string]any,error){beforeInfo:=map[string]any{};e,err:=engine.Open(dir,true);if err!=nil{return nil,err};if err=e.Verify();err!=nil{e.Close();return nil,err};blocks:=e.BlocksCopy();before:=e.Status();if err=e.Close();err!=nil{return nil,err};tmp:=dir+".compact.tmp";_ = os.RemoveAll(tmp);st,err:=storage.Open(tmp);if err!=nil{return nil,err};for _,b:=range blocks{if err=st.AppendBlock(b);err!=nil{st.Close();return nil,err}};if err=st.Close();err!=nil{return nil,err};check,err:=engine.Open(tmp,true);if err!=nil{return nil,err};if err=check.Verify();err!=nil{check.Close();return nil,err};after:=check.Status();check.Close();oldData:=filepath.Join(dir,"data");backupData:=filepath.Join(dir,"data.precompact");_ = os.RemoveAll(backupData);if err=os.Rename(oldData,backupData);err!=nil{return nil,err};if err=os.Rename(filepath.Join(tmp,"data"),oldData);err!=nil{_ = os.Rename(backupData,oldData);return nil,err};_ = os.RemoveAll(tmp);_ = os.RemoveAll(backupData);beforeInfo["verified"]=true;beforeInfo["height"]=before.Height;beforeInfo["transactions"]=before.Transactions;beforeInfo["storage_bytes_before"]=before.StorageBytes;beforeInfo["storage_bytes_after"]=after.StorageBytes;beforeInfo["state_root"]=before.StateRoot;beforeInfo["latest_hash"]=before.LatestHash;return beforeInfo,nil}
func BootstrapReplica(source,target string)(map[string]any,error){tmp:=filepath.Join(os.TempDir(),fmt.Sprintf("agidb-replica-%d.agibak",time.Now().UnixNano()));path,sum,err:=Backup(source,tmp,"");if err!=nil{return nil,err};defer os.Remove(path);if err=RestoreBackup(path,target,"");err!=nil{return nil,err};e,err:=engine.Open(target,true);if err!=nil{return nil,err};defer e.Close();if err=e.Verify();err!=nil{return nil,err};s:=e.Status();return map[string]any{"replica_bootstrapped":true,"source":source,"target":target,"backup_sha256":sum,"height":s.Height,"latest_hash":s.LatestHash,"state_root":s.StateRoot},nil}

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/global-fintech/agidb/internal/engine"
	"github.com/global-fintech/agidb/internal/enterprise"
	"github.com/global-fintech/agidb/internal/merkle"
	"github.com/global-fintech/agidb/internal/model"
	"github.com/global-fintech/agidb/internal/server"
)

func Run(args []string) error {
	if len(args) == 0 { help(); return nil }
	switch args[0] {
	case "init": return initDB(args)
	case "put": return put(args)
	case "delete": return del(args)
	case "get": return get(args)
	case "status": return status(args)
	case "verify": return verify(args)
	case "serve": return serve(args)
	case "execution-commit": return executionCommit(args)
	case "proof-get": return proofGet(args)
	case "proof-verify": return proofVerify(args)
	case "snapshot": return snapshot(args)
	case "snapshot-verify": return snapshotVerify(args)
	case "backup": return backup(args)
	case "backup-verify": return backupVerify(args)
	case "restore": return restore(args)
	case "policy-init": return policyInit(args)
	case "policy-show": return policyShow(args)
	case "prune": return prune(args)
	case "compact": return compact(args)
	case "metrics": return metrics(args)
	case "migrate-plan": return migratePlan(args)
	case "replica-bootstrap": return replicaBootstrap(args)
	case "cluster-init": return clusterInit(args)
	case "cluster-status": return clusterStatus(args)
	case "leader-elect": return leaderElect(args)
	case "quorum-test": return quorumTest(args)
	case "unlock": return unlock(args)
	default: help(); return fmt.Errorf("unknown command %q", args[0])
	}
}

func help() {
	fmt.Println(`AGIDB On-Chain public preview

Commands:
  init, put, delete, get, status, verify, serve
  execution-commit, proof-get, proof-verify
  snapshot, snapshot-verify
  backup, backup-verify, restore
  policy-init, policy-show, prune, compact, metrics, migrate-plan
  replica-bootstrap, cluster-init, cluster-status, leader-elect, quorum-test, unlock`)
}

func arg(args []string, name, fallback string) string { for i:=1;i+1<len(args);i++ { if args[i]==name { return args[i+1] } }; return fallback }
func intArg(args []string,name string,fallback int) int { n,e:=strconv.Atoi(arg(args,name,"")); if e!=nil{return fallback};return n }
func open(args []string, air bool)(*engine.Engine,error){return engine.Open(arg(args,"--dir","./agidb-data"),air)}
func printJSON(v any) error { b,e:=json.MarshalIndent(v,"","  ");if e!=nil{return e};fmt.Println(string(b));return nil }
func noChange(e *engine.Engine, err error) error { if engine.IsNoStateChange(err){s:=e.Status();fmt.Printf("unchanged: duplicate/no-op write suppressed height=%d state_root=%s\n",s.Height,s.StateRoot);return nil};return err }

func initDB(args []string) error { e,err:=open(args,true);if err!=nil{return err};defer e.Close();fmt.Println("initialized",arg(args,"--dir","./agidb-data"));return nil }
func put(args []string) error { if len(args)<3{return errors.New("usage: put KEY VALUE [--dir PATH]")};e,err:=open(args,true);if err!=nil{return err};defer e.Close();_,err=e.Commit([]model.Transaction{engine.NewTransaction([]model.Operation{{Key:args[1],Value:[]byte(args[2])}},"cli",0,nil)});return noChange(e,err) }
func del(args []string) error { if len(args)<2{return errors.New("usage: delete KEY [--dir PATH]")};e,err:=open(args,true);if err!=nil{return err};defer e.Close();_,err=e.Commit([]model.Transaction{engine.NewTransaction([]model.Operation{{Key:args[1],Delete:true}},"cli",0,nil)});return noChange(e,err) }
func get(args []string) error { if len(args)<2{return errors.New("usage: get KEY [--height N] [--dir PATH]")};e,err:=open(args,true);if err!=nil{return err};defer e.Close();h:=intArg(args,"--height",0);var v []byte;var ok bool;if h>0{v,ok=e.GetAt(args[1],uint64(h))}else{v,ok=e.Get(args[1])};if !ok{return os.ErrNotExist};fmt.Println(string(v));return nil }
func status(args []string) error { e,err:=open(args,true);if err!=nil{return err};defer e.Close();return printJSON(e.Status()) }
func verify(args []string) error { e,err:=open(args,true);if err!=nil{return err};defer e.Close();start:=time.Now();if err=e.Verify();err!=nil{return err};s:=e.Status();fmt.Printf("verified: OK elapsed=%s height=%d transactions=%d\n",time.Since(start),s.Height,s.Transactions);return nil }
func serve(args []string) error { if arg(args,"--airgap","")!=""{return errors.New("--airgap refuses network startup")};e,err:=open(args,false);if err!=nil{return err};defer e.Close();addr:=arg(args,"--addr","127.0.0.1:7319");fmt.Println("listening on",addr);return http.ListenAndServe(addr,(&server.Server{Engine:e,Token:arg(args,"--token","")}).Handler()) }
func executionCommit(args []string) error { file:=arg(args,"--file","");if file==""{return errors.New("--file required")};b,err:=os.ReadFile(file);if err!=nil{return err};var x engine.ExecutionBundle;if err=json.Unmarshal(b,&x);err!=nil{return err};e,err:=open(args,true);if err!=nil{return err};defer e.Close();block,err:=e.CommitExecution(x);if engine.IsNoStateChange(err){return noChange(e,err)};if err!=nil{return err};fmt.Printf("execution block committed height=%d hash=%s operations=%d\n",block.Header.Height,block.Header.RecordHash,len(block.Transactions[0].Ops));return nil }
func proofGet(args []string) error { if len(args)<2{return errors.New("usage: proof-get KEY --out FILE")};out:=arg(args,"--out","");if out==""{return errors.New("--out required")};e,err:=open(args,true);if err!=nil{return err};defer e.Close();p:=e.StateProof(args[1]);if !p.Exists{return fmt.Errorf("key not found: %s; non-inclusion proofs are not supported by the current Merkle tree",args[1])};b,_:=json.MarshalIndent(p,"","  ");if err=os.WriteFile(out,b,0o600);err!=nil{return err};fmt.Println("proof written:",out);return nil }
func proofVerify(args []string) error { if len(args)<2{return errors.New("usage: proof-verify FILE")};b,err:=os.ReadFile(args[1]);if err!=nil{return err};var p merkle.Proof;if err=json.Unmarshal(b,&p);err!=nil{return err};if !merkle.VerifyProof(p){return errors.New("proof verification failed")};fmt.Printf("proof verified: OK key=%s root=%s\n",p.Key,p.Root);return nil }
func snapshot(args []string) error { e,err:=open(args,true);if err!=nil{return err};defer e.Close();path,s,err:=e.Snapshot();if err!=nil{return err};fmt.Printf("snapshot created path=%s height=%d keys=%d state_root=%s checksum=%s\n",path,s.Height,len(s.State),s.StateRoot,s.Checksum);return nil }
func snapshotVerify(args []string) error { if len(args)<2{return errors.New("usage: snapshot-verify FILE")};e,err:=open(args,true);if err!=nil{return err};defer e.Close();s,err:=e.VerifySnapshot(args[1]);if err!=nil{return err};fmt.Printf("snapshot verified: OK path=%s height=%d keys=%d state_root=%s checksum=%s\n",args[1],s.Height,len(s.State),s.StateRoot,s.Checksum);return nil }
func backup(args []string) error { path,sum,err:=enterprise.Backup(arg(args,"--dir","./agidb-data"),arg(args,"--out",""),arg(args,"--passphrase",""));if err!=nil{return err};fmt.Printf("backup created path=%s sha256=%s encrypted=%t\n",path,sum,arg(args,"--passphrase","")!="");return nil }
func backupVerify(args []string) error { if len(args)<2{return errors.New("usage: backup-verify FILE")};r,err:=enterprise.VerifyBackup(args[1],arg(args,"--passphrase",""));if err!=nil{return err};return printJSON(r) }
func restore(args []string) error { if len(args)<2{return errors.New("usage: restore FILE --target PATH")};target:=arg(args,"--target","");if err:=enterprise.RestoreBackup(args[1],target,arg(args,"--passphrase",""));err!=nil{return err};e,err:=engine.Open(target,true);if err!=nil{return err};defer e.Close();if err=e.Verify();err!=nil{return err};fmt.Printf("restore verified: OK target=%s height=%d\n",target,e.Status().Height);return nil }
func policyInit(args []string) error { p:=enterprise.DefaultPolicy();p.Mode=arg(args,"--mode","archive");path,err:=enterprise.WritePolicy(arg(args,"--dir","./agidb-data"),p);if err!=nil{return err};fmt.Println("policy created:",path);return printJSON(p) }
func policyShow(args []string) error { p,err:=enterprise.ReadPolicy(arg(args,"--dir","./agidb-data"));if err!=nil{return err};return printJSON(p) }
func prune(args []string) error { r,err:=enterprise.PruneFiles(arg(args,"--dir","./agidb-data"),intArg(args,"--keep-snapshots",8),intArg(args,"--keep-backups",8));if err!=nil{return err};r["canonical_blocks_removed"]=0;fmt.Println("safe maintenance pruning completed; canonical blockchain history was not removed");return printJSON(r) }
func compact(args []string) error { r,err:=enterprise.Compact(arg(args,"--dir","./agidb-data"));if err!=nil{return err};fmt.Println("compaction verified and committed atomically");return printJSON(r) }
func metrics(args []string) error { e,err:=open(args,true);if err!=nil{return err};defer e.Close();start:=time.Now();vErr:=e.Verify();s:=e.Status();return printJSON(map[string]any{"agidb_height":s.Height,"agidb_transactions_total":s.Transactions,"agidb_operations_total":s.Operations,"agidb_keys":s.Keys,"agidb_segments":s.Segments,"agidb_snapshots":s.Snapshots,"agidb_storage_bytes":s.StorageBytes,"agidb_verify_seconds":time.Since(start).Seconds(),"agidb_integrity_ok":vErr==nil,"agidb_noop_suppression":true}) }
func migratePlan(args []string) error { dir:=arg(args,"--dir","./agidb-data");e,err:=engine.Open(dir,true);if err!=nil{return err};s:=e.Status();vErr:=e.Verify();e.Close();path,err:=enterprise.MigrationReport(dir,arg(args,"--target","AGI5"),map[string]any{"source_format":s.FormatVersion,"height":s.Height,"transactions":s.Transactions,"verified":vErr==nil});if err!=nil{return err};fmt.Println("migration plan written:",path);return nil }
func replicaBootstrap(args []string) error { target:=arg(args,"--target","");if target==""{return errors.New("--target required")};r,err:=enterprise.BootstrapReplica(arg(args,"--dir","./agidb-data"),target);if err!=nil{return err};return printJSON(r) }
func splitCSV(s string)[]string{if strings.TrimSpace(s)==""{return nil};parts:=strings.Split(s,",");out:=make([]string,0,len(parts));for _,p:=range parts{if p=strings.TrimSpace(p);p!=""{out=append(out,p)}};return out}
func clusterInit(args []string) error { if len(args)<2{return errors.New("usage: cluster-init NODE_ID")};c,path,err:=enterprise.InitCluster(arg(args,"--dir","./agidb-data"),args[1],splitCSV(arg(args,"--peers","")));if err!=nil{return err};fmt.Println("cluster configuration created:",path);return printJSON(c) }
func clusterStatus(args []string) error { c,err:=enterprise.ReadCluster(arg(args,"--dir","./agidb-data"));if err!=nil{return err};return printJSON(c) }
func leaderElect(args []string) error { c,err:=enterprise.ElectLeader(arg(args,"--dir","./agidb-data"),splitCSV(arg(args,"--candidates","")));if err!=nil{return err};fmt.Println("deterministic test leader elected; this is not production network consensus");return printJSON(c) }
func quorumTest(args []string) error { c,err:=enterprise.ReadCluster(arg(args,"--dir","./agidb-data"));if err!=nil{return err};return printJSON(enterprise.SimulateQuorum(c,intArg(args,"--nodes",len(c.Peers)+1),intArg(args,"--acks",0))) }
func unlock(args []string) error { err:=enterprise.Unlock(arg(args,"--dir","./agidb-data"));if os.IsNotExist(err){fmt.Println("no lock present");return nil};if err==nil{fmt.Println("lock removed; ensure no AGIDB process is active")};return err }

package storage

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/global-fintech/agidb/internal/model"
)

const (
	magic uint32 = 0x41474932
	maxRecordSize = 256 << 20
	defaultSegmentSize = 256 << 20
)

var crcTable = crc32.MakeTable(crc32.Castagnoli)

type Location struct { Segment uint64; Offset int64; Length uint32 }
type Files struct { Dir string; lockPath string; mu sync.Mutex; segmentSize int64; activeID uint64; active *os.File; activeSize int64; index map[uint64]Location }

func Open(dir string) (*Files, error) {
	for _, p := range []string{"data", "wal", "meta", "snapshots", "backups", "cluster"} {
		if err := os.MkdirAll(filepath.Join(dir, p), 0o700); err != nil { return nil, err }
	}
	lockPath := filepath.Join(dir, "meta", "LOCK")
	lf, lockErr := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if lockErr != nil { return nil, fmt.Errorf("database lock unavailable (%s): %w; use agidb unlock only after confirming no process is active", lockPath, lockErr) }
	_, _ = fmt.Fprintf(lf, "pid=%d\n", os.Getpid()); _ = lf.Sync(); _ = lf.Close()
	f := &Files{Dir: dir, lockPath: lockPath, segmentSize: defaultSegmentSize, index: map[uint64]Location{}}
	if err := f.recoverSegments(); err != nil { return nil, err }
	if err := f.openActive(); err != nil { return nil, err }
	return f, nil
}
func (f *Files) segmentPath(id uint64) string { return filepath.Join(f.Dir, "data", fmt.Sprintf("segment-%020d.agi", id)) }
func (f *Files) walPath() string { return filepath.Join(f.Dir, "wal", "commit.wal") }
func encodeBlock(b model.Block) ([]byte, error) { var x bytes.Buffer; err := gob.NewEncoder(&x).Encode(b); return x.Bytes(), err }
func decodeBlock(p []byte) (model.Block, error) { var b model.Block; err := gob.NewDecoder(bytes.NewReader(p)).Decode(&b); return b, err }
func compressPayload(payload []byte) ([]byte, error) { var out bytes.Buffer; w,err:=zlib.NewWriterLevel(&out,zlib.BestSpeed); if err!=nil{return nil,err}; if _,err=w.Write(payload);err!=nil{return nil,err}; if err=w.Close();err!=nil{return nil,err}; return out.Bytes(),nil }
func frame(payload []byte) []byte { encoded,err:=compressPayload(payload); if err!=nil||len(encoded)>=len(payload){encoded=payload}; schema:=uint32(2); if len(encoded)==len(payload){schema=1}; out:=make([]byte,16+len(encoded)); binary.BigEndian.PutUint32(out[0:4],magic); binary.BigEndian.PutUint32(out[4:8],uint32(len(encoded))); binary.BigEndian.PutUint32(out[8:12],crc32.Checksum(encoded,crcTable)); binary.BigEndian.PutUint32(out[12:16],schema); copy(out[16:],encoded); return out }
func readFrame(r io.Reader)([]byte,int64,error){h:=make([]byte,16);n,err:=io.ReadFull(r,h);if errors.Is(err,io.EOF){return nil,0,io.EOF};if err!=nil{return nil,int64(n),err};if binary.BigEndian.Uint32(h[0:4])!=magic{return nil,16,fmt.Errorf("invalid AGI segment magic")};l:=binary.BigEndian.Uint32(h[4:8]);if l==0||l>maxRecordSize{return nil,16,fmt.Errorf("invalid record size %d",l)};p:=make([]byte,l);n2,err:=io.ReadFull(r,p);if err!=nil{return nil,int64(16+n2),err};if crc32.Checksum(p,crcTable)!=binary.BigEndian.Uint32(h[8:12]){return nil,int64(16+l),fmt.Errorf("CRC32C mismatch")};if binary.BigEndian.Uint32(h[12:16])==2{zr,er:=zlib.NewReader(bytes.NewReader(p));if er!=nil{return nil,int64(16+l),er};decoded,er:=io.ReadAll(zr);zr.Close();if er!=nil{return nil,int64(16+l),er};p=decoded};return p,int64(16+l),nil}
func (f *Files) recoverSegments() error { entries,err:=os.ReadDir(filepath.Join(f.Dir,"data"));if err!=nil{return err};ids:=[]uint64{};for _,e:=range entries{if e.IsDir()||!strings.HasPrefix(e.Name(),"segment-"){continue};s:=strings.TrimSuffix(strings.TrimPrefix(e.Name(),"segment-"),".agi");id,_:=strconv.ParseUint(s,10,64);ids=append(ids,id)};sort.Slice(ids,func(i,j int)bool{return ids[i]<ids[j]});for _,id:=range ids{path:=f.segmentPath(id);file,err:=os.OpenFile(path,os.O_RDWR,0o600);if err!=nil{return err};var off int64;for{p,n,er:=readFrame(file);if errors.Is(er,io.EOF){break};if er!=nil{if trunc:=file.Truncate(off);trunc!=nil{file.Close();return fmt.Errorf("recover %s: %w",path,er)};break};b,er:=decodeBlock(p);if er!=nil{file.Close();return er};f.index[b.Header.Height]=Location{Segment:id,Offset:off,Length:uint32(n)};off+=n};file.Close();if id>f.activeID{f.activeID=id}};return nil }
func (f *Files) openActive() error { if f.activeID==0{f.activeID=1};p:=f.segmentPath(f.activeID);x,err:=os.OpenFile(p,os.O_CREATE|os.O_RDWR|os.O_APPEND,0o600);if err!=nil{return err};st,err:=x.Stat();if err!=nil{x.Close();return err};f.active=x;f.activeSize=st.Size();return nil }
func (f *Files) rotate() error { if f.active!=nil{if err:=f.active.Sync();err!=nil{return err};if err:=f.active.Close();err!=nil{return err}};f.activeID++;return f.openActive() }
func (f *Files) AppendBlock(block model.Block) error { f.mu.Lock();defer f.mu.Unlock();p,err:=encodeBlock(block);if err!=nil{return err};fr:=frame(p);if int64(len(fr))>f.segmentSize{return fmt.Errorf("record exceeds segment size")};if f.activeSize+int64(len(fr))>f.segmentSize{if err:=f.rotate();err!=nil{return err}};off:=f.activeSize;if _,err=f.active.Write(fr);err!=nil{return err};if err=f.active.Sync();err!=nil{return err};f.activeSize+=int64(len(fr));f.index[block.Header.Height]=Location{Segment:f.activeID,Offset:off,Length:uint32(len(fr))};return nil }
func (f *Files) ReadAll()([]model.Block,error){heights:=make([]uint64,0,len(f.index));for h:=range f.index{heights=append(heights,h)};sort.Slice(heights,func(i,j int)bool{return heights[i]<heights[j]});out:=make([]model.Block,0,len(heights));for _,h:=range heights{b,err:=f.ReadBlock(h);if err!=nil{return nil,err};out=append(out,b)};return out,nil}
func (f *Files) ReadBlock(height uint64)(model.Block,error){loc,ok:=f.index[height];if !ok{return model.Block{},os.ErrNotExist};x,err:=os.Open(f.segmentPath(loc.Segment));if err!=nil{return model.Block{},err};defer x.Close();if _,err=x.Seek(loc.Offset,io.SeekStart);err!=nil{return model.Block{},err};p,_,err:=readFrame(bufio.NewReader(x));if err!=nil{return model.Block{},err};return decodeBlock(p)}
func (f *Files) WriteWAL(block model.Block) error {p,err:=encodeBlock(block);if err!=nil{return err};tmp:=f.walPath()+".tmp";x,err:=os.OpenFile(tmp,os.O_CREATE|os.O_TRUNC|os.O_WRONLY,0o600);if err!=nil{return err};if _,err=x.Write(frame(p));err!=nil{x.Close();return err};if err=x.Sync();err!=nil{x.Close();return err};if err=x.Close();err!=nil{return err};return os.Rename(tmp,f.walPath())}
func (f *Files) ReadWAL()(*model.Block,error){x,err:=os.Open(f.walPath());if errors.Is(err,os.ErrNotExist){return nil,nil};if err!=nil{return nil,err};defer x.Close();p,_,err:=readFrame(x);if err!=nil{return nil,err};b,err:=decodeBlock(p);return &b,err}
func (f *Files) ClearWAL() error {err:=os.Remove(f.walPath());if errors.Is(err,os.ErrNotExist){return nil};return err}
func (f *Files) Close() error {f.mu.Lock();defer f.mu.Unlock();if f.active==nil{return nil};err:=f.active.Sync();if closeErr:=f.active.Close();err==nil{err=closeErr};f.active=nil;if rmErr:=os.Remove(f.lockPath);err==nil&&!errors.Is(rmErr,os.ErrNotExist){err=rmErr};return err}
func (f *Files) SegmentCount() int {seen:=map[uint64]struct{}{};for _,l:=range f.index{seen[l.Segment]=struct{}{}};if len(seen)==0&&f.activeID>0{return 1};return len(seen)}
func (f *Files) StorageBytes() int64 {entries,err:=os.ReadDir(filepath.Join(f.Dir,"data"));if err!=nil{return 0};var total int64;for _,e:=range entries{if e.IsDir(){continue};if info,err:=e.Info();err==nil{total+=info.Size()}};return total}
func (f *Files) SnapshotCount() int {entries,err:=os.ReadDir(filepath.Join(f.Dir,"snapshots"));if err!=nil{return 0};n:=0;for _,e:=range entries{if !e.IsDir()&&strings.HasSuffix(e.Name(),".agsnap"){n++}};return n}
func (f *Files) WriteSnapshot(s model.Snapshot)(string,error){var payload bytes.Buffer;if err:=gob.NewEncoder(&payload).Encode(s);err!=nil{return "",err};name:=fmt.Sprintf("snapshot-%020d.agsnap",s.Height);path:=filepath.Join(f.Dir,"snapshots",name);tmp:=path+".tmp";x,err:=os.OpenFile(tmp,os.O_CREATE|os.O_TRUNC|os.O_WRONLY,0o600);if err!=nil{return "",err};if _,err=x.Write(frame(payload.Bytes()));err!=nil{x.Close();return "",err};if err=x.Sync();err!=nil{x.Close();return "",err};if err=x.Close();err!=nil{return "",err};if err=os.Rename(tmp,path);err!=nil{return "",err};return path,nil}
func (f *Files) ReadSnapshot(path string)(model.Snapshot,error){if path==""{return model.Snapshot{},errors.New("snapshot path is required")};x,err:=os.Open(path);if err!=nil{return model.Snapshot{},err};defer x.Close();p,_,err:=readFrame(x);if err!=nil{return model.Snapshot{},err};var snap model.Snapshot;if err:=gob.NewDecoder(bytes.NewReader(p)).Decode(&snap);err!=nil{return model.Snapshot{},err};return snap,nil}

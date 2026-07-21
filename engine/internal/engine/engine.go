package engine

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/global-fintech/agidb/internal/merkle"
	"github.com/global-fintech/agidb/internal/model"
	"github.com/global-fintech/agidb/internal/storage"
)

var ErrNoStateChange = errors.New("state unchanged: duplicate/no-op write suppressed")

func IsNoStateChange(err error) bool { return errors.Is(err, ErrNoStateChange) }

type Engine struct {
	mu        sync.RWMutex
	store     *storage.Files
	blocks    []model.Block
	state     map[string][]byte
	versions  map[string][]version
	airGapped bool
	txCount   uint64
	opCount   uint64
}

type version struct {
	Height  uint64
	Value   []byte
	Deleted bool
}

func Open(dir string, airGapped bool) (*Engine, error) {
	store, err := storage.Open(dir)
	if err != nil { return nil, err }
	e := &Engine{store: store, state: map[string][]byte{}, versions: map[string][]version{}, airGapped: airGapped}
	blocks, err := store.ReadAll()
	if err != nil { return nil, err }
	for _, b := range blocks {
		if err := e.applyLoaded(b); err != nil { return nil, err }
	}
	if wal, err := store.ReadWAL(); err != nil {
		return nil, err
	} else if wal != nil {
		if len(e.blocks) == 0 || e.blocks[len(e.blocks)-1].Header.RecordHash != wal.Header.RecordHash {
			if err := store.AppendBlock(*wal); err != nil { return nil, err }
			if err := e.applyLoaded(*wal); err != nil { return nil, err }
		}
		_ = store.ClearWAL()
	}
	return e, nil
}

func (e *Engine) applyLoaded(b model.Block) error {
	expectedHeight := uint64(len(e.blocks) + 1)
	if b.Header.Height != expectedHeight { return fmt.Errorf("height discontinuity: got %d want %d", b.Header.Height, expectedHeight) }
	prev := ""
	if len(e.blocks) > 0 { prev = e.blocks[len(e.blocks)-1].Header.RecordHash }
	if b.Header.PrevHash != prev { return fmt.Errorf("previous hash mismatch at height %d", b.Header.Height) }
	if hashBlock(b) != b.Header.RecordHash { return fmt.Errorf("record hash mismatch at height %d", b.Header.Height) }
	for _, tx := range b.Transactions {
		for _, op := range tx.Ops {
			if op.Delete { delete(e.state, op.Key) } else { e.state[op.Key] = append([]byte{}, op.Value...) }
			e.versions[op.Key] = append(e.versions[op.Key], version{Height: b.Header.Height, Value: append([]byte{}, op.Value...), Deleted: op.Delete})
		}
	}
	if merkle.StateRoot(e.state) != b.Header.StateRoot { return fmt.Errorf("state root mismatch at height %d", b.Header.Height) }
	if b.Header.Schema >= 3 {
		expectedContractRoot := merkle.PrefixRoot(e.state, "contract/")
		if b.Header.Schema >= 4 { expectedContractRoot = contractAggregateRoot(e.state) }
		if expectedContractRoot != b.Header.ContractRoot { return fmt.Errorf("contract root mismatch at height %d", b.Header.Height) }
		if b.Header.Schema >= 4 {
			if merkle.PrefixRoot(e.state, "account/") != b.Header.AccountRoot { return fmt.Errorf("account root mismatch at height %d", b.Header.Height) }
			if merkle.PrefixRoot(e.state, "contract/code/") != b.Header.ContractCodeRoot { return fmt.Errorf("contract code root mismatch at height %d", b.Header.Height) }
			if merkle.PrefixRoot(e.state, "contract-storage/") != b.Header.ContractStorageRoot { return fmt.Errorf("contract storage root mismatch at height %d", b.Header.Height) }
			if merkle.PrefixRoot(e.state, "log/") != b.Header.LogRoot { return fmt.Errorf("log root mismatch at height %d", b.Header.Height) }
		}
		if merkle.PrefixRoot(e.state, "zk/") != b.Header.ZKRoot { return fmt.Errorf("zk root mismatch at height %d", b.Header.Height) }
		if merkle.PrefixRoot(e.state, "receipt/") != b.Header.ReceiptRoot { return fmt.Errorf("receipt root mismatch at height %d", b.Header.Height) }
	}
	for _, tx := range b.Transactions { e.txCount++; e.opCount += uint64(len(tx.Ops)) }
	e.blocks = append(e.blocks, b)
	return nil
}

func NewTransaction(ops []model.Operation, sender string, nonce uint64, payload []byte) model.Transaction {
	var idBytes [16]byte
	_, _ = rand.Read(idBytes[:])
	return model.Transaction{ID: hex.EncodeToString(idBytes[:]), Timestamp: time.Now().UTC(), Sender: sender, Nonce: nonce, Ops: ops, Payload: payload}
}

func (e *Engine) Commit(txs []model.Transaction) (model.Block, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(txs) == 0 { return model.Block{}, errors.New("empty block") }
	nextState := cloneState(e.state)
	filteredTxs := make([]model.Transaction, 0, len(txs))
	for _, tx := range txs {
		if tx.ID == "" { return model.Block{}, errors.New("transaction id required") }
		filteredOps := make([]model.Operation, 0, len(tx.Ops))
		for _, op := range tx.Ops {
			if op.Key == "" { return model.Block{}, errors.New("empty key") }
			current, exists := nextState[op.Key]
			if op.Delete {
				if !exists { continue }
				delete(nextState, op.Key)
				filteredOps = append(filteredOps, op)
				continue
			}
			if exists && bytes.Equal(current, op.Value) { continue }
			nextState[op.Key] = append([]byte{}, op.Value...)
			filteredOps = append(filteredOps, op)
		}
		if len(filteredOps) == 0 { continue }
		tx.Ops = filteredOps
		filteredTxs = append(filteredTxs, tx)
	}
	if len(filteredTxs) == 0 { return model.Block{}, ErrNoStateChange }
	txLeaves := make([][]byte, 0, len(filteredTxs))
	for _, tx := range filteredTxs { b, _ := json.Marshal(tx); txLeaves = append(txLeaves, b) }
	height := uint64(len(e.blocks) + 1)
	prev := ""
	if len(e.blocks) > 0 { prev = e.blocks[len(e.blocks)-1].Header.RecordHash }
	block := model.Block{Header: model.BlockHeader{Height: height, Timestamp: time.Now().UTC(), PrevHash: prev, TxRoot: merkle.Root(txLeaves), StateRoot: merkle.StateRoot(nextState), ContractRoot: contractAggregateRoot(nextState), AccountRoot: merkle.PrefixRoot(nextState, "account/"), ContractCodeRoot: merkle.PrefixRoot(nextState, "contract/code/"), ContractStorageRoot: merkle.PrefixRoot(nextState, "contract-storage/"), LogRoot: merkle.PrefixRoot(nextState, "log/"), ZKRoot: merkle.PrefixRoot(nextState, "zk/"), ReceiptRoot: merkle.PrefixRoot(nextState, "receipt/"), Schema: 4, Compression: "zlib-fast"}, Transactions: filteredTxs}
	block.Header.RecordHash = hashBlock(block)
	if err := e.store.WriteWAL(block); err != nil { return model.Block{}, err }
	if err := e.store.AppendBlock(block); err != nil { return model.Block{}, err }
	for _, tx := range filteredTxs {
		for _, op := range tx.Ops {
			if op.Delete { delete(e.state, op.Key) } else { e.state[op.Key] = append([]byte{}, op.Value...) }
			e.versions[op.Key] = append(e.versions[op.Key], version{Height: height, Value: append([]byte{}, op.Value...), Deleted: op.Delete})
		}
		e.txCount++
		e.opCount += uint64(len(tx.Ops))
	}
	e.blocks = append(e.blocks, block)
	if err := e.store.ClearWAL(); err != nil { return block, err }
	return block, nil
}

func (e *Engine) Get(key string) ([]byte, bool) { e.mu.RLock(); defer e.mu.RUnlock(); v, ok := e.state[key]; return append([]byte{}, v...), ok }
func (e *Engine) GetAt(key string, height uint64) ([]byte, bool) {
	e.mu.RLock(); defer e.mu.RUnlock()
	list := e.versions[key]
	for i := len(list)-1; i >= 0; i-- {
		if list[i].Height <= height {
			if list[i].Deleted { return nil, false }
			return append([]byte{}, list[i].Value...), true
		}
	}
	return nil, false
}
func (e *Engine) Block(height uint64) (model.Block, bool) { e.mu.RLock(); defer e.mu.RUnlock(); if height == 0 || height > uint64(len(e.blocks)) { return model.Block{}, false }; return e.blocks[height-1], true }
func (e *Engine) Status() model.Status {
	e.mu.RLock(); defer e.mu.RUnlock()
	s := model.Status{Height:uint64(len(e.blocks)), Keys:len(e.state), Transactions:e.txCount, Operations:e.opCount, AirGapped:e.airGapped, Verified:true, StateRoot:merkle.StateRoot(e.state), Segments:e.store.SegmentCount(), StorageBytes:e.store.StorageBytes(), Snapshots:e.store.SnapshotCount(), ContractRoot:contractAggregateRoot(e.state), AccountRoot:merkle.PrefixRoot(e.state,"account/"), ContractCodeRoot:merkle.PrefixRoot(e.state,"contract/code/"), ContractStorageRoot:merkle.PrefixRoot(e.state,"contract-storage/"), LogRoot:merkle.PrefixRoot(e.state,"log/"), ZKRoot:merkle.PrefixRoot(e.state,"zk/"), ReceiptRoot:merkle.PrefixRoot(e.state,"receipt/"), Accounts:merkle.PrefixCount(e.state,"account/"), Contracts:countContracts(e.state), ContractSlots:merkle.PrefixCount(e.state,"contract-storage/"), ZKFields:merkle.PrefixCount(e.state,"zk/"), Receipts:merkle.PrefixCount(e.state,"receipt/"), Logs:merkle.PrefixCount(e.state,"log/"), Storage:"AGI3/AGI4 compatible segments + CRC32C + WAL + roots + snapshots + no-op suppression + proofs + enterprise controls", Engine:"AGI-DB Enterprise v10", Compression:"zlib-fast adaptive", FormatVersion:"AGI4", Durability:"WAL fsync + segment fsync per committed block"}
	if len(e.blocks) > 0 { s.LatestHash = e.blocks[len(e.blocks)-1].Header.RecordHash }
	return s
}
func (e *Engine) Verify() error { e.mu.RLock(); defer e.mu.RUnlock(); temp:=&Engine{state:map[string][]byte{},versions:map[string][]version{}}; for _,b:=range e.blocks { if err:=temp.applyLoaded(b); err!=nil{return err} }; return nil }
func (e *Engine) Snapshot() (string, model.Snapshot, error) {
	e.mu.RLock(); defer e.mu.RUnlock(); state:=cloneState(e.state)
	s:=model.Snapshot{Format:"AGISNAP1",CreatedAt:time.Now().UTC(),Height:uint64(len(e.blocks)),StateRoot:merkle.StateRoot(state),State:state,BlockCount:uint64(len(e.blocks)),TxCount:e.txCount}
	if len(e.blocks)>0{s.LatestHash=e.blocks[len(e.blocks)-1].Header.RecordHash}
	checksumPayload,_:=json.Marshal(struct{Height uint64; Hash,Root string; Keys int}{s.Height,s.LatestHash,s.StateRoot,len(state)})
	s.Checksum=merkle.Hash(checksumPayload)
	path,err:=e.store.WriteSnapshot(s)
	return path,s,err
}
func (e *Engine) CommitOperations(ops []model.Operation, sender string, batchSize int) ([]model.Block,error) {
	if batchSize<=0{batchSize=1000}; blocks:=make([]model.Block,0,(len(ops)+batchSize-1)/batchSize)
	for start:=0; start<len(ops); start+=batchSize { end:=start+batchSize; if end>len(ops){end=len(ops)}; txs:=make([]model.Transaction,0,end-start); for i,op:=range ops[start:end]{txs=append(txs,NewTransaction([]model.Operation{op},sender,uint64(start+i),nil))}; b,err:=e.Commit(txs); if IsNoStateChange(err){continue}; if err!=nil{return blocks,err}; blocks=append(blocks,b) }
	return blocks,nil
}
func hashBlock(b model.Block) string { b.Header.RecordHash=""; payload,_:=json.Marshal(b); return merkle.Hash(payload) }
func cloneState(src map[string][]byte) map[string][]byte { dst:=make(map[string][]byte,len(src)); for k,v:=range src{dst[k]=append([]byte{},v...)}; return dst }
func countContracts(s map[string][]byte) int { n:=0; for k:=range s{if len(k)>14 && k[:14]=="contract/code/"{n++}}; return n }
func (e *Engine) PutContract(address string, code, abi []byte)(model.Block,error){ops:=[]model.Operation{{Key:"contract/code/"+address,Value:code}}; if len(abi)>0{ops=append(ops,model.Operation{Key:"contract/abi/"+address,Value:abi})}; tx:=NewTransaction(ops,"contract-deployer",0,nil); tx.Kind="contract_deploy"; return e.Commit([]model.Transaction{tx})}
func (e *Engine) PutContractStorage(address,slot string,value []byte)(model.Block,error){tx:=NewTransaction([]model.Operation{{Key:"contract-storage/"+address+"/"+slot,Value:value}},"contract-runtime",0,nil); tx.Kind="contract_storage"; return e.Commit([]model.Transaction{tx})}
func (e *Engine) PutZKField(circuit,name string,value []byte)(model.Block,error){tx:=NewTransaction([]model.Operation{{Key:"zk/"+circuit+"/"+name,Value:value}},"zk-module",0,nil); tx.Kind="zk_field"; return e.Commit([]model.Transaction{tx})}
func (e *Engine) PutReceipt(txid string,receipt []byte)(model.Block,error){if !json.Valid(receipt){return model.Block{},errors.New("receipt must be valid JSON")}; tx:=NewTransaction([]model.Operation{{Key:"receipt/"+txid,Value:receipt}},"execution",0,nil); tx.Kind="receipt"; return e.Commit([]model.Transaction{tx})}
func contractAggregateRoot(s map[string][]byte) string { merged:=map[string][]byte{}; for k,v:=range s{if strings.HasPrefix(k,"contract/")||strings.HasPrefix(k,"contract-storage/")||strings.HasPrefix(k,"account/"){merged[k]=v}}; return merkle.StateRoot(merged) }
func (e *Engine) PutAccount(address string,accountJSON []byte)(model.Block,error){if !json.Valid(accountJSON){return model.Block{},errors.New("account must be valid JSON")}; tx:=NewTransaction([]model.Operation{{Key:"account/"+strings.ToLower(address),Value:accountJSON}},"execution",0,nil); tx.Kind="account_state"; return e.Commit([]model.Transaction{tx})}
func (e *Engine) PutLog(txid string,index uint64,logJSON []byte)(model.Block,error){if !json.Valid(logJSON){return model.Block{},errors.New("log must be valid JSON")}; key:=fmt.Sprintf("log/%s/%020d",txid,index); tx:=NewTransaction([]model.Operation{{Key:key,Value:logJSON}},"execution",0,nil); tx.Kind="event_log"; return e.Commit([]model.Transaction{tx})}
func (e *Engine) QueryLogs(address,topic string) map[string]string { e.mu.RLock(); defer e.mu.RUnlock(); out:=map[string]string{}; address=strings.ToLower(address); topic=strings.ToLower(topic); for k,v:=range e.state{if !strings.HasPrefix(k,"log/"){continue}; text:=strings.ToLower(string(v)); if address!=""&&!strings.Contains(text,address){continue}; if topic!=""&&!strings.Contains(text,topic){continue}; out[k]=string(v)}; return out }
var zkModuli=map[string]*big.Int{"bn254":mustBig("21888242871839275222246405745257275088548364400416034343698204186575808495617"),"bls12-381-scalar":mustBig("52435875175126190479447740508185965837690552500527637822603658699938581184513")}
func mustBig(s string)*big.Int{n,_:=new(big.Int).SetString(s,10);return n}
func ValidateFieldValue(field,value string) error { mod,ok:=zkModuli[strings.ToLower(field)]; if !ok{return fmt.Errorf("unsupported ZK field %q",field)}; base:=10; raw:=value; if strings.HasPrefix(raw,"0x"){base=16;raw=strings.TrimPrefix(raw,"0x")}; n,ok:=new(big.Int).SetString(raw,base); if !ok||n.Sign()<0{return errors.New("invalid non-negative field element")}; if n.Cmp(mod)>=0{return fmt.Errorf("field element is outside %s modulus",field)}; return nil }
func (e *Engine) PutValidatedZKField(field,circuit,name,value string)(model.Block,error){if err:=ValidateFieldValue(field,value);err!=nil{return model.Block{},err}; payload,_:=json.Marshal(map[string]string{"field":field,"value":value}); return e.PutZKField(circuit,name,payload)}
func (e *Engine) VerifySnapshot(path string)(model.Snapshot,error){s,err:=e.store.ReadSnapshot(path);if err!=nil{return model.Snapshot{},err};if s.Format!="AGISNAP1"{return model.Snapshot{},fmt.Errorf("unsupported snapshot format %q",s.Format)};if merkle.StateRoot(s.State)!=s.StateRoot{return model.Snapshot{},errors.New("snapshot state root mismatch")};checksumPayload,_:=json.Marshal(struct{Height uint64; Hash,Root string; Keys int}{s.Height,s.LatestHash,s.StateRoot,len(s.State)});if merkle.Hash(checksumPayload)!=s.Checksum{return model.Snapshot{},errors.New("snapshot checksum mismatch")};return s,nil}
func (e *Engine) Close() error{return e.store.Close()}
func (e *Engine) StateProof(key string) merkle.Proof{e.mu.RLock();defer e.mu.RUnlock();return merkle.StateProof(e.state,key)}
func (e *Engine) StateCopy() map[string][]byte{e.mu.RLock();defer e.mu.RUnlock();return cloneState(e.state)}
func (e *Engine) BlocksCopy() []model.Block{e.mu.RLock();defer e.mu.RUnlock();out:=make([]model.Block,len(e.blocks));copy(out,e.blocks);return out}
type ExecutionBundle struct{BlockID string `json:"block_id"`;Sender string `json:"sender"`;Nonce uint64 `json:"nonce"`;Accounts map[string]json.RawMessage `json:"accounts,omitempty"`;ContractCode map[string]string `json:"contract_code,omitempty"`;Storage map[string]string `json:"storage,omitempty"`;Receipts map[string]json.RawMessage `json:"receipts,omitempty"`;Logs map[string]json.RawMessage `json:"logs,omitempty"`;State map[string]string `json:"state,omitempty"`}
func (e *Engine) CommitExecution(x ExecutionBundle)(model.Block,error){ops:=make([]model.Operation,0);for address,raw:=range x.Accounts{if !json.Valid(raw){return model.Block{},fmt.Errorf("invalid account JSON for %s",address)};ops=append(ops,model.Operation{Key:"account/"+strings.ToLower(address),Value:append([]byte{},raw...)})};for address,code:=range x.ContractCode{ops=append(ops,model.Operation{Key:"contract/code/"+strings.ToLower(address),Value:[]byte(code)})};for key,value:=range x.Storage{ops=append(ops,model.Operation{Key:"contract-storage/"+key,Value:[]byte(value)})};for txid,raw:=range x.Receipts{if !json.Valid(raw){return model.Block{},fmt.Errorf("invalid receipt JSON for %s",txid)};ops=append(ops,model.Operation{Key:"receipt/"+txid,Value:append([]byte{},raw...)})};for key,raw:=range x.Logs{if !json.Valid(raw){return model.Block{},fmt.Errorf("invalid log JSON for %s",key)};ops=append(ops,model.Operation{Key:"log/"+key,Value:append([]byte{},raw...)})};for key,value:=range x.State{ops=append(ops,model.Operation{Key:key,Value:[]byte(value)})};sort.Slice(ops,func(i,j int)bool{return ops[i].Key<ops[j].Key});tx:=NewTransaction(ops,x.Sender,x.Nonce,nil);tx.Kind="execution_block";if x.BlockID!=""{tx.ID=x.BlockID};return e.Commit([]model.Transaction{tx})}

package model

import "time"

type Operation struct {
	Key    string `json:"key"`
	Value  []byte `json:"value"`
	Delete bool   `json:"delete,omitempty"`
}
type Transaction struct {
	ID        string      `json:"id"`
	Timestamp time.Time   `json:"timestamp"`
	Sender    string      `json:"sender,omitempty"`
	Nonce     uint64      `json:"nonce,omitempty"`
	Ops       []Operation `json:"ops"`
	Payload   []byte      `json:"payload,omitempty"`
	Kind      string      `json:"kind,omitempty"`
}
type BlockHeader struct {
	Height              uint64    `json:"height"`
	Timestamp           time.Time `json:"timestamp"`
	PrevHash            string    `json:"prev_hash"`
	TxRoot              string    `json:"tx_root"`
	StateRoot           string    `json:"state_root"`
	ContractRoot        string    `json:"contract_root,omitempty"`
	AccountRoot         string    `json:"account_root,omitempty"`
	ContractCodeRoot    string    `json:"contract_code_root,omitempty"`
	ContractStorageRoot string    `json:"contract_storage_root,omitempty"`
	LogRoot             string    `json:"log_root,omitempty"`
	ZKRoot              string    `json:"zk_root,omitempty"`
	ReceiptRoot         string    `json:"receipt_root,omitempty"`
	RecordHash          string    `json:"record_hash"`
	Schema              uint32    `json:"schema"`
	Compression         string    `json:"compression,omitempty"`
}
type Block struct {
	Header       BlockHeader   `json:"header"`
	Transactions []Transaction `json:"transactions"`
}
type Snapshot struct {
	Format     string            `json:"format"`
	CreatedAt  time.Time         `json:"created_at"`
	Height     uint64            `json:"height"`
	LatestHash string            `json:"latest_hash"`
	StateRoot  string            `json:"state_root"`
	State      map[string][]byte `json:"state"`
	BlockCount uint64            `json:"block_count"`
	TxCount    uint64            `json:"transaction_count"`
	Checksum   string            `json:"checksum"`
}
type Status struct {
	Height              uint64 `json:"height"`
	LatestHash          string `json:"latest_hash"`
	StateRoot           string `json:"state_root"`
	ContractRoot        string `json:"contract_root"`
	AccountRoot         string `json:"account_root"`
	ContractCodeRoot    string `json:"contract_code_root"`
	ContractStorageRoot string `json:"contract_storage_root"`
	LogRoot             string `json:"log_root"`
	ZKRoot              string `json:"zk_root"`
	ReceiptRoot         string `json:"receipt_root"`
	Keys                int    `json:"keys"`
	Accounts            int    `json:"accounts"`
	Contracts           int    `json:"contracts"`
	ContractSlots       int    `json:"contract_slots"`
	ZKFields            int    `json:"zk_fields"`
	Receipts            int    `json:"receipts"`
	Logs                int    `json:"logs"`
	Transactions        uint64 `json:"transactions"`
	Operations          uint64 `json:"operations"`
	AirGapped           bool   `json:"air_gapped"`
	Verified            bool   `json:"verified"`
	Segments            int    `json:"segments"`
	StorageBytes        int64  `json:"storage_bytes"`
	Snapshots           int    `json:"snapshots"`
	Storage             string `json:"storage"`
	Engine              string `json:"engine"`
	Compression         string `json:"compression"`
	FormatVersion       string `json:"format_version"`
	Durability          string `json:"durability"`
}

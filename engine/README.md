# AGIDB On-Chain Engine

A compressed, append-only, temporal blockchain execution database written in Go.

## Quick start

```powershell
.\agidb-on-chain-windows-amd64.exe init --dir C:\AGIDB\data
.\agidb-on-chain-windows-amd64.exe status --dir C:\AGIDB\data
```

### Native account

```powershell
.\agidb-on-chain-windows-amd64.exe account-put 0xabc '{"nonce":1,"balance":"1000000","code_hash":"0x00"}' --dir C:\AGIDB\data
```

### Contract and storage

```powershell
.\agidb-on-chain-windows-amd64.exe contract-deploy 0xabc 0x60016000 --abi '[{"type":"function","name":"set"}]' --dir C:\AGIDB\data
.\agidb-on-chain-windows-amd64.exe contract-put 0xabc 0x00 42 --dir C:\AGIDB\data
```

### Validated ZK field

```powershell
.\agidb-on-chain-windows-amd64.exe zk-field-put bn254 transfer amount 1000 --dir C:\AGIDB\data
```

### Event log

```powershell
.\agidb-on-chain-windows-amd64.exe log-put tx-1 0 '{"address":"0xabc","topics":["0xdead"],"data":"0x01"}' --dir C:\AGIDB\data
.\agidb-on-chain-windows-amd64.exe log-query --address 0xabc --topic 0xdead --dir C:\AGIDB\data
```

### Verify

```powershell
.\agidb-on-chain-windows-amd64.exe verify --dir C:\AGIDB\data
```

## Integrated commands

```text
execution-commit, proof-get, proof-verify
backup, backup-verify, restore
policy-init, policy-show, prune, compact
metrics, migrate-plan
replica-bootstrap
cluster-init, cluster-status, leader-elect, quorum-test
unlock
```

See `ENTERPRISE_V10.md` for implemented guarantees and production boundaries.

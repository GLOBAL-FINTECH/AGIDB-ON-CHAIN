# AGIDB On-Chain

AGIDB On-Chain is an authenticated, append-only blockchain execution database for EVM, crypto, fintech, audit, and verifiable-state applications.

## Positioning

It is designed as an alternative building block to general-purpose key-value stores when developers need blockchain-oriented guarantees: WAL durability, immutable block history, domain roots, receipts, logs, contract storage, snapshots, proofs, duplicate-write suppression, encrypted backups, verified restore, compaction, and replica bootstrap.

> Public preview. Suitable for development and controlled pilots. Independent security audit, large-scale endurance testing, production network consensus, HSM/KMS integration, and sparse-Merkle non-inclusion proofs remain required for high-value production deployments.

## Downloads

Tagged GitHub releases build and publish:

- `agidb-on-chain-windows-amd64.exe`
- `agidb-on-chain-linux-amd64`
- `SHA256SUMS.txt`

The TypeScript SDK is maintained in `npm/` and is prepared for publication as `@agidb/on-chain`.

## Quick start

```powershell
.\agidb-on-chain-windows-amd64.exe init --dir C:\AGIDB\data
.\agidb-on-chain-windows-amd64.exe serve --dir C:\AGIDB\data --addr 127.0.0.1:7319 --token CHANGE_ME
```

```bash
npm install @agidb/on-chain
```

## Core capabilities

- Authenticated state and domain roots
- Atomic execution bundles
- Account, contract code, contract storage, receipt, log, and ZK namespaces
- Inclusion proofs and proof verification
- No-op and duplicate-write suppression
- CRC32C segments and fsynced WAL
- Snapshots, encrypted backups, restore, compaction, and retention policies
- Metrics, migration planning, replica bootstrap, and quorum/election test framework

## Repository layout

- `engine/` — Go database engine
- `npm/` — TypeScript SDK
- `examples/` — execution examples
- `.github/workflows/` — CI and tagged-release automation

## Production boundary

AGIDB On-Chain is currently a public preview. Its local authenticated-storage, recovery, proof, and execution features can be used in controlled pilots. The quorum and leader-election commands are deterministic test frameworks, not a complete production Raft or Byzantine-fault-tolerant network.

## License

Apache-2.0.

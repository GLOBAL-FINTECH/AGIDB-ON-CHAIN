# @agidb/on-chain

TypeScript client for the AGIDB On-Chain HTTP API.

```bash
npm install @agidb/on-chain
```

```ts
import { AgidbClient } from "@agidb/on-chain";
const db = new AgidbClient({ token: process.env.AGIDB_TOKEN });
await db.commit([{ type: "put", key: "account/0xabc", value: "{\"balance\":\"100\"}" }]);
console.log(await db.status());
```

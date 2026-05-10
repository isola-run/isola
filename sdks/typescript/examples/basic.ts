// Run with: pnpm exec tsx examples/basic.ts
//
// Requires `ISOLA_URL` to point at a running api-gateway.

import { Isola } from "../src";

async function main(): Promise<void> {
  await using client = new Isola();

  await using sandbox = await client.sandboxes.create({ image: "alpine:3.21" });
  const result = await sandbox.commands.run(["echo", "hello world"]);
  console.log("stdout:", result.stdout); // "hello world\n"
  console.log("exitCode:", result.exitCode); // 0
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});

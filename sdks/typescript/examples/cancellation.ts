// Run with: pnpm exec tsx examples/cancellation.ts

import { Isola } from "../src";

async function main(): Promise<void> {
  await using client = new Isola({ requestTimeoutMs: 30_000 });
  await using sandbox = await client.sandboxes.create({ image: "alpine:3.21" });

  // Per-method cancellation via AbortSignal.
  const ac = new AbortController();
  setTimeout(() => ac.abort(new Error("client gave up")), 2_000);

  try {
    const cmd = await sandbox.commands.spawn(["sleep", "30"]);
    await cmd.wait({ timeoutMs: 1_000, signal: ac.signal });
  } catch (err) {
    console.log("Caught:", err);
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});

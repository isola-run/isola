// Run with: pnpm exec tsx examples/streaming.ts

import { Isola } from "../src";

async function main(): Promise<void> {
  await using client = new Isola();
  await using sandbox = await client.sandboxes.create({ image: "alpine:3.21" });

  const cmd = await sandbox.commands.spawn([
    "sh",
    "-c",
    "for i in 1 2 3 4 5; do echo line-$i; sleep 0.5; done",
  ]);

  for await (const chunk of cmd.stdout) {
    process.stdout.write(chunk);
  }

  const code = await cmd.wait();
  console.log("exit code:", code);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});

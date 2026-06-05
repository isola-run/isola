// Run with: pnpm exec tsx examples/rootfs-snapshot.ts

import { Isola } from "../src";

async function main(): Promise<void> {
  const client = new Isola();

  // 1) Create a sandbox and write some state into it.
  const first = await client.sandboxes.create({ image: "alpine:3.21" });
  await first.filesystem.write("/state/note.txt", "hello from snapshot\n");

  // 2) Snapshot the rootfs.
  const snapshot = await client.rootfsSnapshots.create({
    sandboxId: first.id,
    snapshotName: "demo-snapshot",
  });
  console.log("snapshot:", snapshot.id, snapshot.status);

  // 3) Restore the snapshot into a brand-new sandbox.
  await using restored = await client.sandboxes.create({
    image: "alpine:3.21",
    rootfsSnapshotName: snapshot.snapshotName,
  });
  const result = await restored.commands.run(["cat", "/state/note.txt"]);
  console.log("restored:", result.stdout); // "hello from snapshot\n"

  // Cleanup the source sandbox.
  await first.delete();
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});

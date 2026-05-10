// Run with: pnpm exec tsx examples/multi-container.ts

import { Isola } from "../src";

async function main(): Promise<void> {
  await using client = new Isola();
  await using sandbox = await client.sandboxes.create({
    containers: [
      {
        name: "primary",
        image: "alpine:3.21",
        command: ["sleep", "infinity"],
        env: { ROLE: "primary" },
        resources: {
          limits: { cpu: "100m", memory: "128Mi" },
          requests: { cpu: "100m", memory: "128Mi" },
        },
      },
      {
        name: "sidecar",
        image: "alpine:3.21",
        command: ["sleep", "infinity"],
        env: { ROLE: "sidecar" },
        resources: {
          limits: { cpu: "100m", memory: "128Mi" },
          requests: { cpu: "100m", memory: "128Mi" },
        },
      },
    ],
  });

  const primary = await sandbox.commands.run(["sh", "-c", "echo I am $ROLE"], { container: "primary" });
  const sidecar = await sandbox.commands.run(["sh", "-c", "echo I am $ROLE"], { container: "sidecar" });
  console.log(primary.stdout.trim()); // I am primary
  console.log(sidecar.stdout.trim()); // I am sidecar
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});

# Isola TypeScript SDK

Isola is an open-source sandbox platform for running untrusted and AI-generated code securely on your own Kubernetes cluster. This SDK lets you create sandboxes, execute commands, read and write files, and snapshot environments programmatically from TypeScript and JavaScript.

The SDK targets server-side runtimes: Node 22+, Bun, Deno, Cloudflare Workers, and Vercel Edge. There is no browser build; Isola is server-to-server.

The TS SDK is async-only.

## Install

```bash
npm install @isola-run/sdk
# or
pnpm add @isola-run/sdk
# or
yarn add @isola-run/sdk
```

Requires Node 22 or later.

## How it works

- `Isola` is the client. It holds the connection settings to your Isola instance.
- `client.sandboxes.create()` returns a `Sandbox`, where you run code.
- A `Sandbox` exposes `.commands` for executing processes and `.filesystem` for reading and writing files.
- Commands can be blocking (`run`, waits for completion) or non-blocking (`spawn`, streams output as it arrives).
- Rootfs snapshots are separate resources managed through `RootfsSnapshot`.

## Quick start

`ISOLA_URL` must point at your Isola api-gateway. There is no default; set it for every deployment:

```bash
# Local development (kubectl port-forward)
export ISOLA_URL=http://localhost:8080

# In-cluster (same namespace)
export ISOLA_URL=http://isola-api-gateway

# In-cluster (cross-namespace)
export ISOLA_URL=http://isola-api-gateway.isola-system.svc.cluster.local

# External (ingress or load balancer)
export ISOLA_URL=https://isola.example.com
```

Or pass it directly: `new Isola({ url: "http://isola-api-gateway.isola-system.svc.cluster.local" })`.

```ts
import { Isola } from "@isola-run/sdk";

await using client = new Isola();

const sandbox = await client.sandboxes.create({ image: "alpine:3.21" });
const result = await sandbox.commands.run(["echo", "hello world"]);
console.log(result.stdout);     // "hello world\n"
console.log(result.exitCode);   // 0
await sandbox.delete();
```

The `await using` block calls `client[Symbol.asyncDispose]()` automatically when it exits, releasing any in-flight connections. You can also call `await client.close()` instead.

Call `await sandbox.delete()` when you are done with a sandbox. Two alternatives: use `await using sandbox = ...` to delete automatically on exit, or set `timeoutSeconds` on `create()` to let the server delete the sandbox after a fixed duration.

## Sandbox options

Customise resources, environment variables, and the startup command:

```ts
const sandbox = await client.sandboxes.create({
  image: "python:3.12-slim",
  command: ["python", "-m", "http.server", "8080"],
  env: { PORT: "8080", DEBUG: "1" },
  cpu: 0.5,             // CPU cores
  memory: 256,          // MiB
  ephemeralStorage: 1024, // MiB
  timeoutSeconds: 3600, // auto-delete after 1 hour
});
```

Skip waiting for the sandbox to be ready:

```ts
const sandbox = await client.sandboxes.create({ image: "alpine:3.21", maxWaitMs: 0 });
console.log(sandbox.status);  // might be "Pending"
```

> Sandboxes have **no network access by default**. See [Network configuration](#network-configuration) to enable it.

### Timeouts

| Timeout | Side | Default | What it controls |
|---------|------|---------|-----------------|
| `maxWaitMs` | Client | 120_000 (120s) | How long `create()` polls before returning. Set to 0 to return immediately. Raises `IsolaTimeoutError` if it expires. The sandbox keeps running on the server regardless. |
| `startupTimeoutSeconds` | Server | 90s | How long the server gives the sandbox to start. If it expires, the sandbox is marked Failed. Omit to use the server default. |
| `timeoutSeconds` | Server | No limit | Maximum lifetime of the sandbox. The server begins the termination process after this duration. |

> Setting `timeoutSeconds` (or using `await using`) is strongly recommended to ensure the sandbox resource is eventually deleted from the K8s api-server.

### Per-call cancellation

Pass an `AbortSignal` to any method to cancel:

```ts
const ac = new AbortController();
setTimeout(() => ac.abort(new Error("client gave up")), 5_000);
await sandbox.commands.run(["sleep", "30"], {}, { signal: ac.signal });
```

The constructor's `requestTimeoutMs` (default 30_000) bounds each individual HTTP request. A retry resets the budget.

## Commands

### Run (blocking)

`run()` executes a command and waits for it to finish:

```ts
const result = await sandbox.commands.run(["echo", "hello world"]);
console.log(result.stdout);    // "hello world\n"
console.log(result.stderr);    // ""
console.log(result.exitCode);  // 0
```

`run()` does not throw on non-zero exit codes. Always check `result.exitCode`.

```ts
const result = await sandbox.commands.run(
  ["ls", "-la"],
  {
    cwd: "/tmp",
    env: { LANG: "en_US.UTF-8" },
    timeoutSeconds: 30,         // SIGKILL after 30s
  },
);
```

You can also bound the **client-side** wait phase with `waitTimeoutMs`:

```ts
const result = await sandbox.commands.run(["sleep", "100"], { waitTimeoutMs: 5_000 });
// throws IsolaTimeoutError if the command doesn't finish in 5s
```

### Running scripts

Shell scripts: pass to `sh -c`:

```ts
const script = `
echo '== cpu ==';  nproc
echo '== mem ==';  free -h
echo '== disk =='; df -h /
`;
const result = await sandbox.commands.run(["sh", "-c", script]);
```

Python: pass to `python3 -c`:

```ts
const code = `
import json, os
print(json.dumps({"cwd": os.getcwd(), "files": os.listdir(".")}))
`;
const result = await sandbox.commands.run(["python3", "-c", code]);
```

This is the natural pattern when executing LLM-generated code blocks.

> For commands you control, prefer separate args (`run(["python3", "analyze.py", "--input", filename])`): it keeps data separate from the command itself.

### Spawn (non-blocking)

`spawn()` starts a command and returns immediately. Stream output as it arrives:

```ts
const cmd = await sandbox.commands.spawn(["sh", "-c", "for i in 1 2 3; do echo line$i; sleep 1; done"]);
for await (const chunk of cmd.stdout) {
  process.stdout.write(chunk);
}
const exitCode = await cmd.wait();
```

To pass an `AbortSignal` while iterating, use `cmd.stdout.iter({ signal })`:

```ts
const ac = new AbortController();
setTimeout(() => ac.abort(), 1_000);
for await (const chunk of cmd.stdout.iter({ signal: ac.signal })) {
  process.stdout.write(chunk);
}
```

### Stdin

For simple cases, pass `input` to `run()`:

```ts
const result = await sandbox.commands.run(["cat"], { input: "hello from stdin\n" });
console.log(result.stdout);  // "hello from stdin\n"
```

For interactive control, use `writeStdin()` and `closeStdin()` on a spawned command:

```ts
const cmd = await sandbox.commands.spawn(["cat"]);
await cmd.writeStdin("hello\n");
await cmd.closeStdin();
await cmd.wait();
console.log(await cmd.stdout.read());  // "hello\n"
```

### Command control

```ts
const cmd = await sandbox.commands.spawn(["sleep", "60"]);
await cmd.exitCode();  // null (still running)
await cmd.kill();
await cmd.wait();      // returns exit code
```

`cmd.wait({ timeoutMs })` lets you bound the client-side wait deadline:

```ts
try {
  await cmd.wait({ timeoutMs: 5_000 });
} catch (err) {
  if (err instanceof IsolaTimeoutError) {
    await cmd.kill();
  }
}
```

## File I/O

```ts
// Write text
await sandbox.filesystem.write("/tmp/hello.txt", "Hello, World!");

// Write bytes
await sandbox.filesystem.write("/tmp/data.bin", new Uint8Array([0, 1, 2]));

// Stream a file body (Node's `Readable.toWeb()` works too)
await sandbox.filesystem.write("/tmp/upload.tar.gz", fileBlob);

// Read a file
const data = await sandbox.filesystem.read("/tmp/hello.txt");
console.log(new TextDecoder().decode(data));  // "Hello, World!"
```

Parent directories are created automatically on uploads. Streaming bodies (`ReadableStream`) are supported but **non-replayable**: transient errors during a stream upload are not retried.

## Sandbox management

```ts
// List sandboxes
const summaries = await client.sandboxes.list();
for (const s of summaries) {
  console.log(s.id, s.status);
}

// Get a sandbox by ID
const sandbox = await client.sandboxes.get("sandbox-id");
console.log(sandbox.status);             // "Running"
console.log(sandbox.creationTimestamp);  // Date

// Delete explicitly
await sandbox.delete();
```

## Network configuration

Sandboxes have no network access by default. Pass a `network` option to `create()` to open things up.

Allow full internet access:

```ts
const sandbox = await client.sandboxes.create({
  image: "alpine:3.21",
  network: { allowInternetEgress: true },
});
```

When internet egress or custom CIDRs are enabled without cluster DNS, the server automatically configures public nameservers (8.8.8.8, 1.1.1.1) so DNS resolution works out of the box.

Other network options:

```ts
network: {
  allowInternetEgress: false,             // block outbound internet traffic (default)
  allowedEgressCIDRs: ["104.16.0.0/12"],  // fine-grained CIDR allowlist
  allowClusterDNS: false,                 // use the cluster's DNS service
  nameservers: ["8.8.8.8"],               // custom DNS nameservers
  allowIPv6Egress: false,                 // extend egress config to IPv6
}
```

The acronym field names (`allowedEgressCIDRs`, `allowClusterDNS`, `allowIPv6Egress`) match the OpenAPI casing; do not lowercase them.

## Rootfs snapshots

> Requires rootfs snapshots to be enabled and a storage bucket configured in your Helm values (`operator.sandboxRuntime.rootfssnapshot`).

Rootfs snapshots capture one container's root filesystem changes so you can restore them later in a new sandbox. Useful for pre-warming environments: install dependencies once, snapshot, then spin up fresh sandboxes from that snapshot.

### Create a snapshot

```ts
const snapshot = await client.rootfsSnapshots.create({
  sandboxId: sandbox.id,
  snapshotName: "my-snapshot",
});
console.log(snapshot.status);  // "Succeeded"
```

`create()` blocks up to `maxWaitMs` (default 310_000) until the snapshot completes. Pass `maxWaitMs: 0` to return immediately.

### Restore from a snapshot

```ts
const restored = await client.sandboxes.create({
  image: "alpine:3.21",
  rootfsSnapshotName: "my-snapshot",
});
```

### Full round-trip example

```ts
import { Isola } from "@isola-run/sdk";

await using client = new Isola();

// 1. Install a heavy stack once, with internet connectivity.
{
  await using sandbox = await client.sandboxes.create({
    image: "python:3.12-slim",
    network: { allowInternetEgress: true },
    ephemeralStorage: 4096,
  });
  await sandbox.commands.run(["pip", "install", "numpy", "pandas", "scikit-learn"]);
  await client.rootfsSnapshots.create({
    sandboxId: sandbox.id,
    snapshotName: "datascience-base",
  });
}

// 2. Restore from the snapshotted rootfs.
{
  await using sandbox = await client.sandboxes.create({
    image: "python:3.12-slim",
    ephemeralStorage: 4096,
    rootfsSnapshotName: "datascience-base",
  });
  const result = await sandbox.commands.run([
    "python3",
    "-c",
    `
from sklearn.datasets import load_iris
from sklearn.ensemble import RandomForestClassifier
X, y = load_iris(return_X_y=True)
print(RandomForestClassifier(random_state=0).fit(X, y).score(X, y))
`,
  ]);
  console.log(result.stdout);
}
```

### Automatic snapshots on termination

A sandbox can be configured to snapshot automatically as part of its termination policy:

```ts
const sandbox = await client.sandboxes.create({
  image: "alpine:3.21",
  terminationPolicy: { snapshotName: "on-exit-snapshot" },
});
```

The SDK wraps this on the wire as `{ type: "SnapshotRootfs", snapshotRootfs: <input> }`.

### Checking snapshot status

```ts
const snapshot = await client.rootfsSnapshots.get(snapshot.id);
console.log(snapshot.status);  // "Succeeded"
```

## Multi-container sandboxes

For advanced use cases, you can run multiple containers in a single sandbox. Use the `containers` option instead of `image`:

```ts
const limits = {
  limits: { cpu: "500m", memory: "256Mi", ephemeralStorage: "1Gi" },
  requests: { cpu: "500m", memory: "256Mi", ephemeralStorage: "1Gi" },
};

const sandbox = await client.sandboxes.create({
  containers: [
    {
      name: "app",
      image: "python:3.12-slim",
      command: ["python", "-m", "http.server", "8080"],
      resources: limits,
    },
    {
      name: "worker",
      image: "alpine:3.21",
      resources: limits,
    },
  ],
});
```

Set CPU, memory, and ephemeral storage limits on every container. gVisor runs a single sentry process inside the pod cgroup, which is where limits apply to the sandbox. Kubernetes sums container limits into the pod cgroup only when every container declares one, so a missing limit on any container produces surprising pod-level behavior on that dimension: unbounded for CPU and memory, too-low caps for ephemeral storage.

Target a specific container when running commands or writing files:

```ts
const result = await sandbox.commands.run(["wget", "-qO-", "http://127.0.0.1:8080"], { container: "worker" });
await sandbox.filesystem.write("/tmp/data.txt", "hello", { container: "app" });
```

## Error handling

API and SDK errors inherit from `IsolaError`:

```
IsolaError
├── APIError
│   ├── BadRequestError
│   ├── NotFoundError
│   ├── ConflictError
│   ├── ValidationError
│   ├── InternalError
│   └── BadGatewayError
├── IsolaTimeoutError
└── APIConnectionError
```

```ts
import {
  APIConnectionError,
  IsolaError,
  IsolaTimeoutError,
  NotFoundError,
} from "@isola-run/sdk";

try {
  await client.sandboxes.get("nonexistent");
} catch (err) {
  if (err instanceof NotFoundError) {
    console.log(err.statusCode);  // 404
    console.log(err.message);
  } else if (err instanceof IsolaTimeoutError) {
    console.log("Timed out waiting");
  } else if (err instanceof APIConnectionError) {
    console.log("Could not reach the API");
  } else if (err instanceof IsolaError) {
    console.log("Something else went wrong");
  }
}
```

The SDK automatically retries on transient errors (HTTP 502/503/504, transport failures): up to 6 total attempts, fixed 1 s delay between attempts. Per-attempt timeout is governed by `requestTimeoutMs`.

## Migration notes for Python users

- TypeScript SDK is async-only; use `await`, not `with`.
- Use `await using` for automatic cleanup (Node 22+, TC39 explicit resource management).
- All argument names are camelCase (`maxWaitMs`, `startupTimeoutSeconds`, `rootfsSnapshotName`).
- Polling deadlines are in **milliseconds** (`maxWaitMs`), not seconds.
- The constructor's `requestTimeoutMs` is a single wall-clock budget per HTTP attempt; Python's httpx uses four separate buckets (connect/read/write/pool).
- `cmd.wait({ timeoutMs })` and `commands.run({ waitTimeoutMs })` are TS-only knobs (Python parity follow-up).

## License

Apache 2.0. See `../../LICENSE` at the repo root.

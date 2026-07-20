// Copyright The Isola Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Mirrors sdks/python/tests/test_commands.py functionally, same scenarios,
// same assertions where the abstractions line up.

import { describe, expect, it } from "vitest";
import { Isola } from "../src/client";
import { BadGatewayError, InternalError, IsolaTimeoutError, NotFoundError } from "../src/errors";
import {
  emptyResponse,
  getSearchParam,
  hangUntilAbort,
  jsonResponse,
  makeRoutingFetch,
  makeStubFetch,
  sandboxResponseFixture,
} from "./_helpers";

const URL_BASE = "http://localhost:8080";

describe("Commands.spawn", () => {
  it("posts the full payload and includes ?container query param", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ id: "00000000-0000-0000-0000-000000000001" }, { status: 202 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const cmd = await sandbox.commands.spawn(["python", "-c", "print('hello')"], {
      env: { DEBUG: "1" },
      cwd: "/workspace",
      timeoutSeconds: 30,
      container: "worker",
    });

    expect(cmd.id).toBe("00000000-0000-0000-0000-000000000001");
    const spawnCall = stub.calls[1];
    expect(spawnCall).toBeDefined();
    expect(spawnCall?.method).toBe("POST");
    expect(getSearchParam(spawnCall?.url ?? "", "container")).toBe("worker");
    expect(JSON.parse(spawnCall?.bodyText ?? "")).toEqual({
      args: ["python", "-c", "print('hello')"],
      env: { DEBUG: "1" },
      cwd: "/workspace",
      timeoutSeconds: 30,
    });
  });

  it("minimal payload omits unset fields and has no container param", async () => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()), jsonResponse({ id: "cmd-5" }, { status: 202 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    await sandbox.commands.spawn(["ls"]);

    const spawnCall = stub.calls[1];
    expect(spawnCall).toBeDefined();
    expect(JSON.parse(spawnCall?.bodyText ?? "")).toEqual({ args: ["ls"] });
    expect(getSearchParam(spawnCall?.url ?? "", "container")).toBeNull();
  });

  it("rejects empty args", async () => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    await expect(sandbox.commands.spawn([])).rejects.toThrow(/at least one argument/);
  });
});

describe("Commands.run", () => {
  it("returns CommandResult with stdout/stderr/exitCode", async () => {
    const sbId = "sandbox-123";
    const cmdId = "cmd-1";

    const routes = {
      [`GET /v1/sandboxes/${sbId}`]: () => jsonResponse(sandboxResponseFixture()),
      [`POST /v1/sandboxes/${sbId}/commands`]: () => jsonResponse({ id: cmdId }, { status: 202 }),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/status`]: () => jsonResponse({ exitCode: 0 }),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/stdout`]: () =>
        new Response("data: hello world\ndata: \nid: 12\n\n", {
          status: 200,
          headers: { "content-type": "text/event-stream" },
        }),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/stderr`]: () =>
        new Response("", { status: 200, headers: { "content-type": "text/event-stream" } }),
    };
    const routing = makeRoutingFetch(routes);
    const client = new Isola({ url: URL_BASE, fetch: routing.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get(sbId);
    const result = await sandbox.commands.run(["echo", "hello world"]);

    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBe("hello world\n");
    expect(result.stderr).toBe("");
    expect(result.id).toBe(cmdId);
  });

  it("does NOT throw on non-zero exit code, returns CommandResult { exitCode: 17 }", async () => {
    // README contract: run() returns a CommandResult; a non-zero exit code is
    // a normal completion, not an error. (Mirrors Python parity.) This pins
    // the behavior so a regression that started throwing on exitCode > 0
    // would fail loudly.
    const sbId = "sandbox-123";
    const cmdId = "cmd-nonzero";
    const routes = {
      [`GET /v1/sandboxes/${sbId}`]: () => jsonResponse(sandboxResponseFixture()),
      [`POST /v1/sandboxes/${sbId}/commands`]: () => jsonResponse({ id: cmdId }, { status: 202 }),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/status`]: () => jsonResponse({ exitCode: 17 }),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/stdout`]: () =>
        new Response("", { status: 200, headers: { "content-type": "text/event-stream" } }),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/stderr`]: () =>
        new Response("data: oops\ndata: \nid: 1\n\n", {
          status: 200,
          headers: { "content-type": "text/event-stream" },
        }),
    };
    const routing = makeRoutingFetch(routes);
    const client = new Isola({ url: URL_BASE, fetch: routing.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get(sbId);
    const result = await sandbox.commands.run(["sh", "-c", "exit 17"]);
    expect(result.exitCode).toBe(17);
    expect(result.stderr).toBe("oops\n");
  });

  it("with input: writes stdin and closes before reading streams", async () => {
    const sbId = "sandbox-123";
    const cmdId = "cmd-input";

    // Track call order for stdin/close vs stdout/stderr/status. In particular:
    // stdin and stdin/close must precede the wait/stdout/stderr group.
    let stdinSeen = false;
    let stdinCloseSeen = false;
    let stdoutOpened = false;
    let stderrOpened = false;
    let waitOpened = false;
    let stdinBody: string | null = null;

    const routes = {
      [`GET /v1/sandboxes/${sbId}`]: () => jsonResponse(sandboxResponseFixture()),
      [`POST /v1/sandboxes/${sbId}/commands`]: () => jsonResponse({ id: cmdId }, { status: 202 }),
      [`POST /v1/sandboxes/${sbId}/commands/${cmdId}/stdin`]: (req: { bodyText: string }) => {
        stdinBody = req.bodyText;
        stdinSeen = true;
        return emptyResponse(204);
      },
      [`POST /v1/sandboxes/${sbId}/commands/${cmdId}/stdin/close`]: () => {
        stdinCloseSeen = true;
        return emptyResponse(204);
      },
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/status`]: () => {
        waitOpened = true;
        // Wait must run only AFTER stdin & close, verify ordering.
        expect(stdinSeen).toBe(true);
        expect(stdinCloseSeen).toBe(true);
        return jsonResponse({ exitCode: 0 });
      },
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/stdout`]: () => {
        stdoutOpened = true;
        expect(stdinSeen).toBe(true);
        expect(stdinCloseSeen).toBe(true);
        return new Response("data: hello\ndata: \nid: 6\n\n", {
          status: 200,
          headers: { "content-type": "text/event-stream" },
        });
      },
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/stderr`]: () => {
        stderrOpened = true;
        expect(stdinSeen).toBe(true);
        expect(stdinCloseSeen).toBe(true);
        return new Response("", {
          status: 200,
          headers: { "content-type": "text/event-stream" },
        });
      },
    };
    const routing = makeRoutingFetch(routes);
    const client = new Isola({ url: URL_BASE, fetch: routing.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get(sbId);
    const result = await sandbox.commands.run(["cat"], { input: "hello\n" });

    expect(stdinBody).toBe("hello\n");
    expect(stdoutOpened).toBe(true);
    expect(stderrOpened).toBe(true);
    expect(waitOpened).toBe(true);
    expect(result.exitCode).toBe(0);
    expect(result.stdout).toBe("hello\n");
  });
});

describe("Command.stdout / Command.stderr", () => {
  it("stdout decodes SSE body to 'hello world\\n' (trailing-newline pattern)", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ id: "cmd-1" }, { status: 202 }),
      new Response("data: hello world\ndata: \nid: 12\n\n", {
        status: 200,
        headers: { "content-type": "text/event-stream" },
      }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const cmd = await sandbox.commands.spawn(["echo", "hello world"]);

    const chunks: string[] = [];
    for await (const chunk of cmd.stdout) {
      chunks.push(chunk);
    }
    expect(chunks.join("")).toBe("hello world\n");
  });

  it("stderr decodes SSE body to 'error output\\n'", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ id: "cmd-2" }, { status: 202 }),
      new Response("data: error output\ndata: \nid: 13\n\n", {
        status: 200,
        headers: { "content-type": "text/event-stream" },
      }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const cmd = await sandbox.commands.spawn(["ls", "nonexistent"]);

    const chunks: string[] = [];
    for await (const chunk of cmd.stderr) {
      chunks.push(chunk);
    }
    expect(chunks.join("")).toBe("error output\n");
  });
});

describe("Command.stdout / Command.stderr lazy caching", () => {
  // Pins the lazy-init memoization in commands.ts so callers can hold a
  // reference to the StreamReader before iterating, without double-fetching.
  it.each([
    { key: "stdout" as const },
    { key: "stderr" as const },
  ])("$key returns the same StreamReader instance on repeated access", async ({ key }) => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()), jsonResponse({ id: "cmd-s" }, { status: 202 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const cmd = await sandbox.commands.spawn(["echo"]);
    expect(cmd[key]).toBe(cmd[key]);
  });
});

describe("Command.exitCode", () => {
  it("GET /status returns 42 (one-shot, no waitSeconds query param)", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ id: "cmd-ec" }, { status: 202 }),
      jsonResponse({ exitCode: 42 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const cmd = await sandbox.commands.spawn(["sh", "-c", "exit 42"]);
    expect(await cmd.exitCode()).toBe(42);

    // exitCode() is the one-shot probe (matches Python `command.exit_code`);
    // it must NOT include the long-poll wait param. wait() is the long-poll
    // companion (and IS pinned at waitSeconds=20 in the wait() suite).
    const statusCall = stub.calls[2];
    expect(getSearchParam(statusCall!.url, "waitSeconds")).toBeNull();
  });

  it("returns null when exitCode is null", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ id: "cmd-ec-n" }, { status: 202 }),
      jsonResponse({ exitCode: null }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const cmd = await sandbox.commands.spawn(["sleep", "10"]);
    expect(await cmd.exitCode()).toBeNull();
  });
});

describe("Command.writeStdin", () => {
  it("string is encoded as UTF-8 bytes", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ id: "cmd-stdin-text" }, { status: 202 }),
      emptyResponse(204),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const cmd = await sandbox.commands.spawn(["cat"]);
    await cmd.writeStdin("hello\n");

    const stdinCall = stub.calls[2];
    expect(stdinCall).toBeDefined();
    expect(stdinCall?.method).toBe("POST");
    expect(stdinCall?.bodyText).toBe("hello\n");
    expect(Array.from(stdinCall?.body ?? [])).toEqual(Array.from(new TextEncoder().encode("hello\n")));
  });

  it("Uint8Array is sent as-is", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ id: "cmd-stdin-bin" }, { status: 202 }),
      emptyResponse(204),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const cmd = await sandbox.commands.spawn(["cat"]);

    const data = new Uint8Array([0xff, 0x00, 0x42, 0x10]);
    await cmd.writeStdin(data);

    const stdinCall = stub.calls[2];
    expect(stdinCall).toBeDefined();
    expect(Array.from(stdinCall?.body ?? [])).toEqual(Array.from(data));
  });
});

describe("Command.closeStdin", () => {
  it("POSTs to /stdin/close with no body", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ id: "cmd-close" }, { status: 202 }),
      emptyResponse(204),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const cmd = await sandbox.commands.spawn(["cat"]);
    await cmd.closeStdin();

    const closeCall = stub.calls[2];
    expect(closeCall).toBeDefined();
    expect(closeCall?.method).toBe("POST");
    expect(new URL(closeCall?.url ?? "").pathname).toBe("/v1/sandboxes/sandbox-123/commands/cmd-close/stdin/close");
    expect(closeCall?.body).toBeNull();
  });
});

describe("Command.kill", () => {
  it("DELETEs /commands/{id}", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ id: "cmd-kill" }, { status: 202 }),
      emptyResponse(204),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const cmd = await sandbox.commands.spawn(["sleep", "100"]);
    await cmd.kill();

    const killCall = stub.calls[2];
    expect(killCall).toBeDefined();
    expect(killCall?.method).toBe("DELETE");
    expect(new URL(killCall?.url ?? "").pathname).toBe("/v1/sandboxes/sandbox-123/commands/cmd-kill");
  });
});

describe("Command.wait", () => {
  it("GETs /status with ?waitSeconds=20", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ id: "cmd-lp" }, { status: 202 }),
      jsonResponse({ exitCode: 0 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const cmd = await sandbox.commands.spawn(["echo", "hi"]);
    const code = await cmd.wait();
    expect(code).toBe(0);

    const statusCall = stub.calls[2];
    expect(statusCall).toBeDefined();
    expect(getSearchParam(statusCall?.url ?? "", "waitSeconds")).toBe("20");
  });

  it("retries on null exitCode until non-null", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ id: "cmd-retry" }, { status: 202 }),
      jsonResponse({ exitCode: null }),
      jsonResponse({ exitCode: 0 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const cmd = await sandbox.commands.spawn(["sleep", "1"]);
    const code = await cmd.wait();
    expect(code).toBe(0);
    // 1 (sandbox.get) + 1 (spawn) + 2 (status retries) = 4
    expect(stub.calls).toHaveLength(4);
  });

  it("throws IsolaTimeoutError when timeoutMs fires", async () => {
    // Use a routing fetch so the status responder always returns null,
    // forcing the wait loop to keep going until AbortSignal.timeout fires.
    const sbId = "sandbox-123";
    const cmdId = "cmd-timeout";
    const routes = {
      [`GET /v1/sandboxes/${sbId}`]: () => jsonResponse(sandboxResponseFixture()),
      [`POST /v1/sandboxes/${sbId}/commands`]: () => jsonResponse({ id: cmdId }, { status: 202 }),
      // Always return null. The status request itself either returns null
      // and loops, OR returns aborted via the AbortSignal once the timeout
      // fires. Here the responder is sync so by the time the abort fires
      // we may have already returned a body, the loop will then start
      // another request with an already-aborted signal, which `fetch`
      // rejects with AbortError, and `wait()` translates that into
      // IsolaTimeoutError because timeoutSignal.aborted is true.
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/status`]: async (req: {
        signal: AbortSignal | undefined;
      }): Promise<Response> => {
        // Yield a macrotask each iteration so the AbortSignal.timeout timer
        // gets a chance to fire. Without this, the wait() loop is purely
        // microtask-based and the timer is starved indefinitely.
        await new Promise<void>((resolve, reject) => {
          if (req.signal?.aborted) {
            reject(req.signal.reason ?? new DOMException("aborted", "AbortError"));
            return;
          }
          const t = setTimeout(resolve, 1);
          req.signal?.addEventListener(
            "abort",
            () => {
              clearTimeout(t);
              reject(req.signal?.reason ?? new DOMException("aborted", "AbortError"));
            },
            { once: true },
          );
        });
        return jsonResponse({ exitCode: null });
      },
    };
    const routing = makeRoutingFetch(routes);
    const client = new Isola({ url: URL_BASE, fetch: routing.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get(sbId);
    const cmd = await sandbox.commands.spawn(["sleep", "100"]);

    let caught: unknown;
    try {
      await cmd.wait({ timeoutMs: 50 });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(IsolaTimeoutError);
    expect((caught as Error).message).toContain(`did not complete within 50ms`);
    expect((caught as Error).message).toContain(cmdId);
  }, 10_000);
});

describe("Commands.run sibling cancellation", () => {
  it("aborts stderr/wait when stdout rejects (e.g. 404 on stdout SSE)", async () => {
    const sbId = "sandbox-123";
    const cmdId = "cmd-fail";

    // Track whether stderr and wait observed an aborted signal, i.e. the
    // sibling cancellation path on the run() catch
    // actually reached them.
    let stderrAborted = false;
    let waitAborted = false;
    const stderrSeen = { count: 0 };
    const waitSeen = { count: 0 };

    const routes = {
      [`GET /v1/sandboxes/${sbId}`]: () => jsonResponse(sandboxResponseFixture()),
      [`POST /v1/sandboxes/${sbId}/commands`]: () => jsonResponse({ id: cmdId }, { status: 202 }),
      // stdout: fail with 404 immediately (non-transient → no retries → bubbles
      // up to run()'s catch).
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/stdout`]: () =>
        jsonResponse({ detail: "stdout not found" }, { status: 404 }),
      // stderr: hang waiting for the internalController abort.
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/stderr`]: (req: { signal: AbortSignal | undefined }) => {
        stderrSeen.count += 1;
        return hangUntilAbort(req, () => {
          stderrAborted = true;
        });
      },
      // wait/status: same, must be aborted by the sibling cancellation.
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/status`]: (req: { signal: AbortSignal | undefined }) => {
        waitSeen.count += 1;
        return hangUntilAbort(req, () => {
          waitAborted = true;
        });
      },
    };
    const routing = makeRoutingFetch(routes);
    const client = new Isola({ url: URL_BASE, fetch: routing.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get(sbId);

    let caught: unknown;
    try {
      await sandbox.commands.run(["echo", "hi"]);
    } catch (err) {
      caught = err;
    }
    // run() rejects with the original 404 NotFoundError from stdout.
    expect(caught).toBeInstanceOf(NotFoundError);
    // The sibling abort path actually reached stderr/wait handlers.
    expect(stderrAborted).toBe(true);
    expect(waitAborted).toBe(true);
    expect(stderrSeen.count).toBeGreaterThanOrEqual(1);
    expect(waitSeen.count).toBeGreaterThanOrEqual(1);
  }, 15_000);
});

describe("Commands.run waitTimeoutMs", () => {
  it("throws IsolaTimeoutError when waitTimeoutMs expires before completion", async () => {
    const sbId = "sandbox-123";
    const cmdId = "cmd-runtimeout";

    const routes = {
      [`GET /v1/sandboxes/${sbId}`]: () => jsonResponse(sandboxResponseFixture()),
      [`POST /v1/sandboxes/${sbId}/commands`]: () => jsonResponse({ id: cmdId }, { status: 202 }),
      // All three streams hang indefinitely; waitTimeoutMs must abort them.
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/stdout`]: (req: { signal: AbortSignal | undefined }) =>
        hangUntilAbort(req),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/stderr`]: (req: { signal: AbortSignal | undefined }) =>
        hangUntilAbort(req),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/status`]: (req: { signal: AbortSignal | undefined }) =>
        hangUntilAbort(req),
    };
    const routing = makeRoutingFetch(routes);
    const client = new Isola({ url: URL_BASE, fetch: routing.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get(sbId);

    let caught: unknown;
    const t0 = performance.now();
    try {
      await sandbox.commands.run(["sleep", "10"], { waitTimeoutMs: 80 });
    } catch (err) {
      caught = err;
    }
    const elapsed = performance.now() - t0;

    expect(caught).toBeInstanceOf(IsolaTimeoutError);
    expect((caught as Error).message).toContain(`did not complete within 80ms`);
    // Sanity: the timeout fired close to its budget, not after every sibling
    // independently timed out the request layer.
    expect(elapsed).toBeLessThan(2_000);
  }, 10_000);

  it("propagates user signal.reason when aborted ahead of waitTimeoutMs", async () => {
    const sbId = "sandbox-123";
    const cmdId = "cmd-userabort-run";
    const reason = new Error("user-cancelled-run");
    const ctrl = new AbortController();

    const routes = {
      [`GET /v1/sandboxes/${sbId}`]: () => jsonResponse(sandboxResponseFixture()),
      [`POST /v1/sandboxes/${sbId}/commands`]: () => jsonResponse({ id: cmdId }, { status: 202 }),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/stdout`]: (req: { signal: AbortSignal | undefined }) =>
        hangUntilAbort(req),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/stderr`]: (req: { signal: AbortSignal | undefined }) =>
        hangUntilAbort(req),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/status`]: (req: { signal: AbortSignal | undefined }) =>
        hangUntilAbort(req),
    };
    const routing = makeRoutingFetch(routes);
    const client = new Isola({ url: URL_BASE, fetch: routing.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get(sbId);

    setTimeout(() => ctrl.abort(reason), 30);

    let caught: unknown;
    try {
      await sandbox.commands.run(["sleep", "10"], { waitTimeoutMs: 5_000 }, { signal: ctrl.signal });
    } catch (err) {
      caught = err;
    }
    // User abort wins over the run-phase deadline.
    expect(caught).toBe(reason);
  }, 10_000);

  it("prefers user signal.reason over waitTimeoutMs when both fire mid-Promise.all", async () => {
    // Race we cover: spawn succeeds, then stdout/stderr/wait
    // hang in Promise.all, and BOTH the waitTimeoutMs deadline AND the user
    // signal abort near-simultaneously. The catch must surface the user's
    // reason, not IsolaTimeoutError. (Mirrors Command.wait's precedence.)
    const sbId = "sandbox-123";
    const cmdId = "cmd-bothfire";
    const reason = new Error("user-cancelled-race");
    const ctrl = new AbortController();

    const routes = {
      [`GET /v1/sandboxes/${sbId}`]: () => jsonResponse(sandboxResponseFixture()),
      [`POST /v1/sandboxes/${sbId}/commands`]: () => jsonResponse({ id: cmdId }, { status: 202 }),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/stdout`]: (req: { signal: AbortSignal | undefined }) =>
        hangUntilAbort(req),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/stderr`]: (req: { signal: AbortSignal | undefined }) =>
        hangUntilAbort(req),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/status`]: (req: { signal: AbortSignal | undefined }) =>
        hangUntilAbort(req),
    };
    const routing = makeRoutingFetch(routes);
    const client = new Isola({ url: URL_BASE, fetch: routing.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get(sbId);

    // Schedule the user abort and the waitTimeoutMs deadline both at ~50ms so
    // they race inside Promise.all's catch (where the user-signal-wins
    // precedence rule lives).
    setTimeout(() => ctrl.abort(reason), 50);

    let caught: unknown;
    try {
      await sandbox.commands.run(["sleep", "10"], { waitTimeoutMs: 50 }, { signal: ctrl.signal });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBe(reason);
  }, 10_000);

  it("accepts input: null without crashing (JS-only foot-gun)", async () => {
    // TS types forbid `input: null`; JS callers can pass it. The run() guard
    // must skip the writeStdin path so we don't hit `fetch` with body: null.
    const sbId = "sandbox-123";
    const cmdId = "cmd-null-input";
    const stub = makeRoutingFetch({
      [`GET /v1/sandboxes/${sbId}`]: () => jsonResponse(sandboxResponseFixture()),
      [`POST /v1/sandboxes/${sbId}/commands`]: () => jsonResponse({ id: cmdId }, { status: 202 }),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/stdout`]: () => emptyResponse(204),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/stderr`]: () => emptyResponse(204),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/status`]: () => jsonResponse({ exitCode: 0 }),
    });
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get(sbId);

    const result = await sandbox.commands.run(["true"], { input: null as never });
    expect(result.exitCode).toBe(0);
    // No POST to /stdin (writeStdin path skipped).
    const stdinCalls = stub.calls.filter((c) => c.url.includes("/stdin"));
    expect(stdinCalls).toHaveLength(0);
  });
});

describe("Command.wait error pass-through", () => {
  it("re-throws non-signal-caused errors verbatim", async () => {
    const sbId = "sandbox-123";
    const cmdId = "cmd-erron-wait";
    const routes = {
      [`GET /v1/sandboxes/${sbId}`]: () => jsonResponse(sandboxResponseFixture()),
      [`POST /v1/sandboxes/${sbId}/commands`]: () => jsonResponse({ id: cmdId }, { status: 202 }),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/status`]: () =>
        jsonResponse({ detail: "sidecar exploded" }, { status: 500 }),
    };
    const routing = makeRoutingFetch(routes);
    const client = new Isola({ url: URL_BASE, fetch: routing.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get(sbId);
    const cmd = await sandbox.commands.spawn(["sleep", "100"]);

    // wait() with no timeout, no signal, the catch path falls through the
    // timeout/signal branches and into the verbatim re-throw.
    await expect(cmd.wait()).rejects.toThrow(InternalError);
  });
});

describe("Command.wait user-signal abort", () => {
  it("propagates user signal.reason when aborted mid-poll", async () => {
    const sbId = "sandbox-123";
    const cmdId = "cmd-userabort";
    const reason = new Error("user-cancelled-wait");
    const ctrl = new AbortController();

    const routes = {
      [`GET /v1/sandboxes/${sbId}`]: () => jsonResponse(sandboxResponseFixture()),
      [`POST /v1/sandboxes/${sbId}/commands`]: () => jsonResponse({ id: cmdId }, { status: 202 }),
      // status: hang until the user signal aborts.
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/status`]: (req: { signal: AbortSignal | undefined }) =>
        hangUntilAbort(req),
    };
    const routing = makeRoutingFetch(routes);
    const client = new Isola({ url: URL_BASE, fetch: routing.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get(sbId);
    const cmd = await sandbox.commands.spawn(["sleep", "100"]);

    // Fire the user abort 10ms after the wait starts.
    setTimeout(() => ctrl.abort(reason), 10);

    let caught: unknown;
    try {
      // No timeoutMs, solely user-signal driven so the catch falls to the
      // opts.signal?.aborted branch.
      await cmd.wait({ signal: ctrl.signal });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBe(reason);
  }, 10_000);
});

describe("Commands error mapping", () => {
  it("spawn 500 -> InternalError", async () => {
    // 500 is non-transient, no retry. One failure responder is enough.
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ detail: "sidecar unreachable" }, { status: 500 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");

    let caught: unknown;
    try {
      await sandbox.commands.spawn(["ls"]);
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(InternalError);
    expect((caught as InternalError).statusCode).toBe(500);
    expect((caught as InternalError).message).toContain("sidecar unreachable");
  });

  it("spawn 502 -> BadGatewayError without retrying the POST", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ detail: "bad gateway" }, { status: 502 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    await expect(sandbox.commands.spawn(["ls"])).rejects.toThrow(BadGatewayError);
    expect(stub.calls).toHaveLength(2);
  });

  it("kill 404 -> NotFoundError", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ id: "cmd-kill-err" }, { status: 202 }),
      jsonResponse({ detail: "command not found" }, { status: 404 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const cmd = await sandbox.commands.spawn(["sleep", "100"]);

    let caught: unknown;
    try {
      await cmd.kill();
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(NotFoundError);
    expect((caught as NotFoundError).message).toContain("command not found");
  });

  it("writeStdin 500 -> InternalError", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ id: "cmd-stdin-err" }, { status: 202 }),
      jsonResponse({ detail: "stdin closed" }, { status: 500 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const cmd = await sandbox.commands.spawn(["cat"]);
    await expect(cmd.writeStdin(new Uint8Array([1, 2, 3]))).rejects.toThrow(InternalError);
  });

  it("closeStdin 404 -> NotFoundError", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ id: "cmd-closestdin-err" }, { status: 202 }),
      jsonResponse({ detail: "command not found" }, { status: 404 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const cmd = await sandbox.commands.spawn(["cat"]);
    await expect(cmd.closeStdin()).rejects.toThrow(NotFoundError);
  });
});

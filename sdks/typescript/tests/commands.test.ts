import { describe, it, expect, beforeEach } from "vitest";
import { Isola } from "../src/isola.js";
import type { CommandStatusResponse, CreateCommandResponse } from "../src/models.js";
import {
  mockFetch,
  installMockFetch,
  jsonResponse,
  noContentResponse,
  sseResponse,
  SANDBOX_DATA,
} from "./helpers.js";

installMockFetch();

beforeEach(() => {
  mockFetch.mockReset();
});

const client = new Isola({ baseURL: "http://localhost:8080" });

async function getSandbox() {
  mockFetch.mockResolvedValueOnce(jsonResponse(SANDBOX_DATA));
  return client.sandboxes.get("sbx-test-123");
}

describe("Commands.spawn", () => {
  it("sends command and returns Command handle", async () => {
    const sandbox = await getSandbox();

    const cmdResp: CreateCommandResponse = { commandId: "cmd-abc" };
    mockFetch.mockResolvedValueOnce(jsonResponse(cmdResp));

    const cmd = await sandbox.commands.spawn(["ls", "-la"]);
    expect(cmd.id).toBe("cmd-abc");

    const [url, init] = mockFetch.mock.calls[1]!;
    expect(url).toBe(
      "http://localhost:8080/v1/sandboxes/sbx-test-123/commands",
    );
    expect(init?.method).toBe("POST");

    const body = JSON.parse(init?.body as string);
    expect(body).toEqual({ args: ["ls", "-la"] });
  });

  it("sends optional env, cwd, timeout", async () => {
    const sandbox = await getSandbox();

    const cmdResp: CreateCommandResponse = { commandId: "cmd-abc" };
    mockFetch.mockResolvedValueOnce(jsonResponse(cmdResp));

    await sandbox.commands.spawn(["python3", "app.py"], {
      env: { DEBUG: "1" },
      cwd: "/app",
      timeout: 60,
    });

    const body = JSON.parse(mockFetch.mock.calls[1]![1]?.body as string);
    expect(body).toEqual({
      args: ["python3", "app.py"],
      env: { DEBUG: "1" },
      cwd: "/app",
      timeout: 60,
    });
  });

  it("sends container as query param", async () => {
    const sandbox = await getSandbox();

    const cmdResp: CreateCommandResponse = { commandId: "cmd-abc" };
    mockFetch.mockResolvedValueOnce(jsonResponse(cmdResp));

    await sandbox.commands.spawn(["ls"], { container: "sidecar" });

    const [url] = mockFetch.mock.calls[1]!;
    expect(url).toContain("container=sidecar");
  });

  it("rejects empty args at compile time", () => {
    // Verify that empty args are rejected by the type system.
    // @ts-expect-error -- empty array is not assignable to [string, ...string[]]
    const _spawn: (args: readonly [string, ...string[]]) => void = (_: readonly []) => {};
    void _spawn;
  });
});

describe("Command.exitCode", () => {
  it("returns exit code when available", async () => {
    const sandbox = await getSandbox();

    mockFetch.mockResolvedValueOnce(
      jsonResponse({ commandId: "cmd-1" } satisfies CreateCommandResponse),
    );
    const cmd = await sandbox.commands.spawn(["true"]);

    mockFetch.mockResolvedValueOnce(
      jsonResponse({ exitCode: 0 } satisfies CommandStatusResponse),
    );
    const code = await cmd.exitCode();
    expect(code).toBe(0);
  });

  it("returns null when still running", async () => {
    const sandbox = await getSandbox();

    mockFetch.mockResolvedValueOnce(
      jsonResponse({ commandId: "cmd-1" } satisfies CreateCommandResponse),
    );
    const cmd = await sandbox.commands.spawn(["sleep", "10"]);

    mockFetch.mockResolvedValueOnce(
      jsonResponse({ exitCode: null } satisfies CommandStatusResponse),
    );
    const code = await cmd.exitCode();
    expect(code).toBeNull();
  });
});

describe("Command.wait", () => {
  it("long-polls until exit code is available", async () => {
    const sandbox = await getSandbox();

    mockFetch.mockResolvedValueOnce(
      jsonResponse({ commandId: "cmd-1" } satisfies CreateCommandResponse),
    );
    const cmd = await sandbox.commands.spawn(["bash"]);

    // First poll: still running
    mockFetch.mockResolvedValueOnce(
      jsonResponse({ exitCode: null } satisfies CommandStatusResponse),
    );
    // Second poll: exited
    mockFetch.mockResolvedValueOnce(
      jsonResponse({ exitCode: 42 } satisfies CommandStatusResponse),
    );

    const code = await cmd.wait();
    expect(code).toBe(42);

    // Verify waitSeconds query param
    const statusCalls = mockFetch.mock.calls.slice(2);
    for (const call of statusCalls) {
      expect(call[0]).toContain("waitSeconds=20");
    }
  });
});

describe("Command.writeStdin / closeStdin", () => {
  it("writes string to stdin", async () => {
    const sandbox = await getSandbox();

    mockFetch.mockResolvedValueOnce(
      jsonResponse({ commandId: "cmd-1" } satisfies CreateCommandResponse),
    );
    const cmd = await sandbox.commands.spawn(["cat"]);

    mockFetch.mockResolvedValueOnce(noContentResponse());
    await cmd.writeStdin("hello world");

    const [url, init] = mockFetch.mock.calls[2]!;
    expect(url).toContain("/commands/cmd-1/stdin");
    expect(init?.method).toBe("POST");
    expect(init?.headers).toEqual(
      expect.objectContaining({ "Content-Type": "application/octet-stream" }),
    );
  });

  it("writes bytes to stdin", async () => {
    const sandbox = await getSandbox();

    mockFetch.mockResolvedValueOnce(
      jsonResponse({ commandId: "cmd-1" } satisfies CreateCommandResponse),
    );
    const cmd = await sandbox.commands.spawn(["cat"]);

    const bytes = new TextEncoder().encode("binary data");
    mockFetch.mockResolvedValueOnce(noContentResponse());
    await cmd.writeStdin(bytes);
  });

  it("closes stdin", async () => {
    const sandbox = await getSandbox();

    mockFetch.mockResolvedValueOnce(
      jsonResponse({ commandId: "cmd-1" } satisfies CreateCommandResponse),
    );
    const cmd = await sandbox.commands.spawn(["cat"]);

    mockFetch.mockResolvedValueOnce(noContentResponse());
    await cmd.closeStdin();

    const [url, init] = mockFetch.mock.calls[2]!;
    expect(url).toContain("/commands/cmd-1/stdin/close");
    expect(init?.method).toBe("POST");
  });
});

describe("Command.kill", () => {
  it("sends DELETE request", async () => {
    const sandbox = await getSandbox();

    mockFetch.mockResolvedValueOnce(
      jsonResponse({ commandId: "cmd-1" } satisfies CreateCommandResponse),
    );
    const cmd = await sandbox.commands.spawn(["sleep", "999"]);

    mockFetch.mockResolvedValueOnce(noContentResponse());
    await cmd.kill();

    const [url, init] = mockFetch.mock.calls[2]!;
    expect(url).toContain("/commands/cmd-1");
    expect(init?.method).toBe("DELETE");
  });
});

describe("Command.stdout / stderr", () => {
  it("returns lazy StreamReader for stdout", async () => {
    const sandbox = await getSandbox();

    mockFetch.mockResolvedValueOnce(
      jsonResponse({ commandId: "cmd-1" } satisfies CreateCommandResponse),
    );
    const cmd = await sandbox.commands.spawn(["echo", "hi"]);

    // Accessing .stdout twice returns the same instance
    const reader1 = cmd.stdout;
    const reader2 = cmd.stdout;
    expect(reader1).toBe(reader2);
  });
});

describe("Commands.run", () => {
  it("spawns, waits, and collects output", async () => {
    const sandbox = await getSandbox();

    // spawn
    mockFetch.mockResolvedValueOnce(
      jsonResponse({ commandId: "cmd-1" } satisfies CreateCommandResponse),
    );
    // wait (exit code)
    mockFetch.mockResolvedValueOnce(
      jsonResponse({ exitCode: 0 } satisfies CommandStatusResponse),
    );
    // stdout SSE
    mockFetch.mockResolvedValueOnce(
      sseResponse("id: 0\ndata: hello world\n\n"),
    );
    // stderr SSE
    mockFetch.mockResolvedValueOnce(
      sseResponse("id: 0\ndata: \n\n"),
    );

    const result = await sandbox.commands.run(["echo", "hello world"]);
    expect(result.commandId).toBe("cmd-1");
    expect(result.stdout).toBe("hello world");
    expect(result.stderr).toBe("");
    expect(result.exitCode).toBe(0);
  });

  it("writes input to stdin before waiting", async () => {
    const sandbox = await getSandbox();

    // spawn
    mockFetch.mockResolvedValueOnce(
      jsonResponse({ commandId: "cmd-1" } satisfies CreateCommandResponse),
    );
    // writeStdin
    mockFetch.mockResolvedValueOnce(noContentResponse());
    // closeStdin
    mockFetch.mockResolvedValueOnce(noContentResponse());
    // wait
    mockFetch.mockResolvedValueOnce(
      jsonResponse({ exitCode: 0 } satisfies CommandStatusResponse),
    );
    // stdout
    mockFetch.mockResolvedValueOnce(sseResponse("id: 0\ndata: echo\n\n"));
    // stderr
    mockFetch.mockResolvedValueOnce(sseResponse(""));

    const result = await sandbox.commands.run(["cat"], { input: "echo" });
    expect(result.stdout).toBe("echo");

    // Verify stdin write was called
    const stdinCall = mockFetch.mock.calls[2]!;
    expect((stdinCall[0] as string)).toContain("/stdin");
    expect(stdinCall[1]?.method).toBe("POST");
  });
});

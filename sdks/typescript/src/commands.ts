import type { APIClient } from "./client.js";
import type {
  CommandResult,
  CommandStatusResponse,
  CreateCommandPayload,
  CreateCommandResponse,
} from "./models.js";
import { StreamReader } from "./streaming.js";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/** Must be <= gateway max (25s). */
const LONG_POLL_WAIT_SECONDS = 20;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function commandBasePath(sandboxId: string): string {
  return `/v1/sandboxes/${encodeURIComponent(sandboxId)}/commands`;
}

function commandPath(sandboxId: string, commandId: string): string {
  return `${commandBasePath(sandboxId)}/${encodeURIComponent(commandId)}`;
}

// ---------------------------------------------------------------------------
// SpawnOptions / RunOptions
// ---------------------------------------------------------------------------

export interface SpawnOptions {
  /** Environment variables for the command. */
  env?: Record<string, string>;
  /** Working directory inside the sandbox. */
  cwd?: string;
  /** Command timeout in seconds (server-enforced). */
  timeout?: number;
  /** Target container (multi-container sandboxes). */
  container?: string;
}

export interface RunOptions extends SpawnOptions {
  /** Data to write to stdin before waiting. Strings are UTF-8 encoded. */
  input?: string | Uint8Array;
}

// ---------------------------------------------------------------------------
// Command
// ---------------------------------------------------------------------------

/**
 * Handle to a running (or finished) command inside a sandbox.
 *
 * Obtained via {@link Commands.spawn}.
 */
export class Command {
  readonly id: string;
  private _stdout: StreamReader | null = null;
  private _stderr: StreamReader | null = null;

  /** @internal */
  constructor(
    private readonly api: APIClient,
    private readonly sandboxId: string,
    commandId: string,
  ) {
    this.id = commandId;
  }

  private get basePath(): string {
    return commandPath(this.sandboxId, this.id);
  }

  // -----------------------------------------------------------------------
  // Streaming output (lazy, single-use)
  // -----------------------------------------------------------------------

  /** SSE stream of the command's stdout. */
  get stdout(): StreamReader {
    if (!this._stdout) {
      this._stdout = new StreamReader(this.api, `${this.basePath}/stdout`);
    }
    return this._stdout;
  }

  /** SSE stream of the command's stderr. */
  get stderr(): StreamReader {
    if (!this._stderr) {
      this._stderr = new StreamReader(this.api, `${this.basePath}/stderr`);
    }
    return this._stderr;
  }

  // -----------------------------------------------------------------------
  // Status
  // -----------------------------------------------------------------------

  /** Poll the exit code once (returns `null` if still running). */
  async exitCode(): Promise<number | null> {
    const resp = await this.api.request<CommandStatusResponse>(
      "GET",
      `${this.basePath}/status`,
    );
    return resp.exitCode;
  }

  /** Long-poll until the command exits, then return its exit code. */
  async wait(): Promise<number> {
    for (;;) {
      const resp = await this.api.request<CommandStatusResponse>(
        "GET",
        `${this.basePath}/status`,
        { params: { waitSeconds: String(LONG_POLL_WAIT_SECONDS) } },
      );
      if (resp.exitCode !== null) {
        return resp.exitCode;
      }
    }
  }

  // -----------------------------------------------------------------------
  // stdin
  // -----------------------------------------------------------------------

  /** Write data to the command's stdin. */
  async writeStdin(data: string | Uint8Array): Promise<void> {
    const content =
      typeof data === "string" ? new TextEncoder().encode(data) : data;
    await this.api.requestNoContent("POST", `${this.basePath}/stdin`, {
      content,
      headers: { "Content-Type": "application/octet-stream" },
    });
  }

  /** Close the command's stdin pipe. */
  async closeStdin(): Promise<void> {
    await this.api.requestNoContent("POST", `${this.basePath}/stdin/close`);
  }

  // -----------------------------------------------------------------------
  // Lifecycle
  // -----------------------------------------------------------------------

  /** Kill the command (idempotent). */
  async kill(): Promise<void> {
    await this.api.requestNoContent("DELETE", this.basePath);
  }
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

/**
 * Command execution on a sandbox.
 *
 * Obtained via {@link Sandbox.commands}.
 */
export class Commands {
  /** @internal */
  constructor(
    private readonly api: APIClient,
    private readonly sandboxId: string,
  ) {}

  /**
   * Start a command without waiting for it to finish.
   *
   * @param args - Command and arguments (e.g. `["ls", "-la"]`).
   *               At least one element is required.
   * @returns A {@link Command} handle for polling status and reading output.
   */
  async spawn(
    args: readonly [string, ...string[]],
    options?: SpawnOptions,
  ): Promise<Command> {
    const payload: CreateCommandPayload = {
      args,
      env: options?.env,
      cwd: options?.cwd,
      timeout: options?.timeout,
    };

    const params: Record<string, string> = {};
    if (options?.container) {
      params.container = options.container;
    }

    const resp = await this.api.request<CreateCommandResponse>(
      "POST",
      commandBasePath(this.sandboxId),
      {
        body: payload,
        params: Object.keys(params).length > 0 ? params : undefined,
      },
    );

    return new Command(this.api, this.sandboxId, resp.commandId);
  }

  /**
   * Run a command to completion and collect its output.
   *
   * Optionally writes {@link RunOptions.input | input} to stdin before
   * waiting. Reads stdout, stderr, and exit code concurrently.
   */
  async run(
    args: readonly [string, ...string[]],
    options?: RunOptions,
  ): Promise<CommandResult> {
    const cmd = await this.spawn(args, options);

    if (options?.input !== undefined) {
      await cmd.writeStdin(options.input);
      await cmd.closeStdin();
    }

    const [exitCode, stdout, stderr] = await Promise.all([
      cmd.wait(),
      cmd.stdout.read(),
      cmd.stderr.read(),
    ]);

    return { commandId: cmd.id, stdout, stderr, exitCode };
  }
}

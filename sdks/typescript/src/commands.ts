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

// Mirrors sdks/python/src/isola/_commands.py:AsyncCommand and AsyncCommands.

import type { RequestOptions } from "./client";
import { IsolaTimeoutError } from "./errors";
import type { HttpClient } from "./internal/http";
import {
  commandBasePath,
  commandPath,
  commandStatusPath,
  commandStderrPath,
  commandStdinClosePath,
  commandStdinPath,
  commandStdoutPath,
} from "./internal/url";
import type { CommandResult } from "./models";
import {
  type CommandStatusResponse,
  CommandStatusResponse as CommandStatusResponseModel,
  type CreateCommandPayload,
  CreateCommandPayload as CreateCommandPayloadModel,
  type CreateCommandResponse,
  CreateCommandResponse as CreateCommandResponseModel,
} from "./models";
import { StreamReader } from "./streaming";

// Long-poll wait. Must stay below the api-gateway's maximum (25s). Part of the
// timeout chain: SDK 20s < gateway 25s < sidecar 30s < gateway WriteTimeout 45s
// < sidecar WriteTimeout 75s.
const LONG_POLL_WAIT_SECONDS = 20;

/** Options for {@link Commands.spawn}. */
export interface SpawnOptions {
  /** Environment variables for the command. */
  env?: Record<string, string>;
  /** Working directory inside the sandbox. */
  cwd?: string;
  /**
   * Maximum time the command can run, in seconds. Enforced
   * server-side. The server kills the process if it runs longer.
   * If omitted, no server-side limit is applied.
   */
  timeoutSeconds?: number;
  /**
   * Target container name. Only needed for multi-container sandboxes.
   */
  container?: string;
}

/** Options for {@link Commands.run}. */
export interface RunOptions extends SpawnOptions {
  /**
   * Data to send to the command's stdin. The SDK writes this and
   * closes stdin automatically. Strings are encoded as UTF-8. For
   * interactive control, use {@link Commands.spawn} with
   * {@link Command.writeStdin} instead.
   */
  input?: string | Uint8Array;
  /**
   * Client-side deadline for the wait/read phase, in milliseconds.
   * Throws {@link IsolaTimeoutError} on expiry.
   */
  waitTimeoutMs?: number;
}

/** Options for {@link Command.wait}. */
export interface WaitOptions {
  /**
   * Client-side wait deadline, in milliseconds. Throws
   * {@link IsolaTimeoutError} on expiry.
   */
  timeoutMs?: number;
  /** AbortSignal for per-call cancellation. */
  signal?: AbortSignal;
}

const STATUS_PARAMS = { waitSeconds: LONG_POLL_WAIT_SECONDS } as const;

function buildWaitSignal(
  userSignal: AbortSignal | undefined,
  timeoutMs: number | undefined,
): { signal: AbortSignal | undefined; timeoutSignal: AbortSignal | undefined } {
  if (timeoutMs === undefined) {
    return { signal: userSignal, timeoutSignal: undefined };
  }
  const timeoutSignal = AbortSignal.timeout(timeoutMs);
  const signals: AbortSignal[] = [];
  if (userSignal) signals.push(userSignal);
  signals.push(timeoutSignal);
  return {
    signal: signals.length === 1 ? signals[0] : AbortSignal.any(signals),
    timeoutSignal,
  };
}

/** Execute commands inside a sandbox. */
export class Commands {
  /** @internal */
  readonly _api: HttpClient;
  /** @internal */
  readonly _sandboxId: string;

  /** @internal */
  constructor(api: HttpClient, sandboxId: string) {
    this._api = api;
    this._sandboxId = sandboxId;
  }

  /**
   * Start a command without waiting for it to finish.
   *
   * @example
   * ```ts
   * const cmd = await sandbox.commands.spawn(["ls", "-la"]);
   * for await (const chunk of cmd.stdout) {
   *   process.stdout.write(chunk);
   * }
   * await cmd.wait();
   * ```
   *
   * @param args - The command and its arguments as separate strings
   * (e.g. `["ls", "-la"]`).
   * @param opts - Spawn options (`env`, `cwd`, `timeoutSeconds`,
   * `container`).
   * @returns A {@link Command} handle for streaming output, sending
   * input, or waiting for completion.
   * @throws {Error} If `args` is empty.
   */
  async spawn(args: string[], opts: SpawnOptions = {}, req: RequestOptions = {}): Promise<Command> {
    if (!Array.isArray(args) || args.length === 0) {
      throw new TypeError("at least one argument (the command) is required");
    }
    const payload: CreateCommandPayload = { args };
    if (opts.env !== undefined) payload.env = opts.env;
    if (opts.cwd !== undefined) payload.cwd = opts.cwd;
    if (opts.timeoutSeconds !== undefined) payload.timeoutSeconds = opts.timeoutSeconds;
    const data = await this._api.requestModel<CreateCommandResponse>(
      {
        method: "POST",
        path: commandBasePath(this._sandboxId),
        ...(opts.container ? { params: { container: opts.container } } : {}),
        jsonBody: CreateCommandPayloadModel.toWire(payload),
        ...(req.signal ? { signal: req.signal } : {}),
      },
      CreateCommandResponseModel.fromWire,
    );
    return new Command(this._api, this._sandboxId, data.id);
  }

  /**
   * Run a command and wait for it to complete.
   *
   * Convenience wrapper around {@link Commands.spawn}: starts the
   * command, optionally sends `input` to stdin, waits for the
   * process to exit, and collects stdout and stderr.
   *
   * @example
   * ```ts
   * const result = await sandbox.commands.run(["echo", "hello"]);
   * console.log(result.stdout); // "hello\n"
   * console.log(result.exitCode); // 0
   * ```
   *
   * @param args - The command and its arguments as separate strings
   * (e.g. `["echo", "hello world"]`).
   * @param opts - Run options. `input` is written to stdin and stdin
   * is then closed automatically.
   * @returns A {@link CommandResult} with `stdout`, `stderr`, and
   * `exitCode`.
   * @throws {Error} If `args` is empty.
   * @throws {IsolaTimeoutError} If `waitTimeoutMs` expires before
   * the command exits.
   */
  async run(args: string[], opts: RunOptions = {}, req: RequestOptions = {}): Promise<CommandResult> {
    const cmd = await this.spawn(args, opts, req);

    // `!= null` (not `!== undefined`) so a JS caller passing `input: null`
    // doesn't crash inside writeStdin. Mirrors Python's `is not None`.
    if (opts.input != null) {
      await cmd.writeStdin(opts.input, req);
      await cmd.closeStdin(req);
    }

    // waitTimeoutMs bounds the entire wait+read phase. Build one AbortSignal
    // composed from the user signal, an internalController (to cancel
    // siblings when any of stdout/stderr/wait rejects), and the optional
    // run-phase deadline; pass it to all three concurrent waits.
    const internalController = new AbortController();
    const signals: AbortSignal[] = [internalController.signal];
    if (req.signal) signals.push(req.signal);
    const runTimeoutSignal = opts.waitTimeoutMs !== undefined ? AbortSignal.timeout(opts.waitTimeoutMs) : undefined;
    if (runTimeoutSignal) signals.push(runTimeoutSignal);
    // signals has >=1 entries (internalController is unconditional); use
    // AbortSignal.any only when there are >=2 to compose.
    const composedSignal: AbortSignal = signals.length === 1 ? internalController.signal : AbortSignal.any(signals);

    try {
      const [stdout, stderr, exitCode] = await Promise.all([
        cmd.stdout.read({ signal: composedSignal }),
        cmd.stderr.read({ signal: composedSignal }),
        cmd.wait({ signal: composedSignal }),
      ]);
      return { id: cmd.id, stdout, stderr, exitCode };
    } catch (err) {
      // Cancel siblings as soon as one rejects so the other reads/wait don't
      // hang on a server that has stopped sending events.
      if (!internalController.signal.aborted) {
        internalController.abort(err instanceof Error ? err : new Error(String(err)));
      }
      // User cancel wins over the run-phase deadline if both fired: caller
      // intent is preserved (mirrors Command.wait's same precedence rule).
      if (req.signal?.aborted) throw req.signal.reason ?? err;
      if (runTimeoutSignal?.aborted) {
        throw new IsolaTimeoutError(`command ${cmd.id} did not complete within ${opts.waitTimeoutMs}ms`, {
          cause: err,
        });
      }
      throw err;
    }
  }
}

/**
 * A running or completed command inside a sandbox.
 *
 * Returned by {@link Commands.spawn}. Use {@link Command.stdout} and
 * {@link Command.stderr} to stream output, {@link Command.wait} to
 * block until completion, or {@link Command.kill} to terminate the
 * process.
 */
export class Command {
  /** @internal */
  readonly _api: HttpClient;
  /** @internal */
  readonly _sandboxId: string;
  /** @internal */
  readonly _commandId: string;
  private _stdout: StreamReader | null = null;
  private _stderr: StreamReader | null = null;

  /** @internal */
  constructor(api: HttpClient, sandboxId: string, commandId: string) {
    this._api = api;
    this._sandboxId = sandboxId;
    this._commandId = commandId;
  }

  /** Unique identifier of the command. */
  get id(): string {
    return this._commandId;
  }

  /**
   * Stream of the command's standard output.
   *
   * Yields text chunks as they arrive. Single-use: iterate once with
   * `for await` or call {@link StreamReader.read} to collect
   * everything.
   */
  get stdout(): StreamReader {
    if (this._stdout === null) {
      this._stdout = new StreamReader(this._api, commandStdoutPath(this._sandboxId, this._commandId));
    }
    return this._stdout;
  }

  /**
   * Stream of the command's standard error.
   *
   * Yields text chunks as they arrive. Single-use: iterate once with
   * `for await` or call {@link StreamReader.read} to collect
   * everything.
   */
  get stderr(): StreamReader {
    if (this._stderr === null) {
      this._stderr = new StreamReader(this._api, commandStderrPath(this._sandboxId, this._commandId));
    }
    return this._stderr;
  }

  /**
   * Poll the command's exit status.
   *
   * @returns The exit code if the command has finished, or `null` if
   * it is still running.
   */
  async exitCode(req: RequestOptions = {}): Promise<number | null> {
    const status = await this._api.requestModel<CommandStatusResponse>(
      {
        method: "GET",
        path: commandStatusPath(this._sandboxId, this._commandId),
        ...(req.signal ? { signal: req.signal } : {}),
      },
      CommandStatusResponseModel.fromWire,
    );
    return status.exitCode;
  }

  /**
   * Block until the command finishes.
   *
   * To bound the wait server-side, pass `timeoutSeconds` to
   * {@link Commands.spawn}. To bound it client-side, pass `timeoutMs`
   * here.
   *
   * @returns The exit code of the command.
   * @throws {IsolaTimeoutError} If `timeoutMs` expires before the
   * command exits.
   */
  async wait(opts: WaitOptions = {}): Promise<number> {
    const path = commandStatusPath(this._sandboxId, this._commandId);
    const { signal, timeoutSignal } = buildWaitSignal(opts.signal, opts.timeoutMs);
    while (true) {
      // Honor abort/timeout fired between iterations before starting another
      // 20s long-poll. Without this, an abort that arrives during status
      // decoding would still trigger one more round-trip.
      if (opts.signal?.aborted) throw opts.signal.reason;
      if (timeoutSignal?.aborted) {
        throw new IsolaTimeoutError(`command ${this._commandId} did not complete within ${opts.timeoutMs}ms`);
      }
      try {
        const status = await this._api.requestModel<CommandStatusResponse>(
          {
            method: "GET",
            path,
            params: STATUS_PARAMS,
            ...(signal ? { signal } : {}),
          },
          CommandStatusResponseModel.fromWire,
        );
        if (status.exitCode !== null) return status.exitCode;
      } catch (err) {
        // User cancel wins over the wait deadline if both fired: caller
        // intent is preserved.
        if (opts.signal?.aborted) throw opts.signal.reason ?? err;
        if (timeoutSignal?.aborted) {
          throw new IsolaTimeoutError(`command ${this._commandId} did not complete within ${opts.timeoutMs}ms`, {
            cause: err,
          });
        }
        throw err;
      }
    }
  }

  /**
   * Send data to the command's standard input.
   *
   * @param data - Text or bytes to write. Strings are encoded as
   * UTF-8.
   * @throws {APIError} If the API returns a non-2xx response.
   * @throws {APIConnectionError} If the request cannot reach the API.
   */
  async writeStdin(data: string | Uint8Array, req: RequestOptions = {}): Promise<void> {
    const raw: Uint8Array = typeof data === "string" ? new TextEncoder().encode(data) : data;
    await this._api.requestNoContent({
      method: "POST",
      path: commandStdinPath(this._sandboxId, this._commandId),
      body: raw,
      headers: { "content-type": "application/octet-stream" },
      ...(req.signal ? { signal: req.signal } : {}),
    });
  }

  /**
   * Close the command's standard input.
   *
   * Call this after writing all input so the command knows there is
   * no more data coming (like pressing Ctrl-D).
   *
   * @throws {APIError} If the API returns a non-2xx response.
   * @throws {APIConnectionError} If the request cannot reach the API.
   */
  async closeStdin(req: RequestOptions = {}): Promise<void> {
    await this._api.requestNoContent({
      method: "POST",
      path: commandStdinClosePath(this._sandboxId, this._commandId),
      ...(req.signal ? { signal: req.signal } : {}),
    });
  }

  /**
   * Terminate the command immediately.
   *
   * @throws {APIError} If the API returns a non-2xx response.
   * @throws {APIConnectionError} If the request cannot reach the API.
   */
  async kill(req: RequestOptions = {}): Promise<void> {
    await this._api.requestNoContent({
      method: "DELETE",
      path: commandPath(this._sandboxId, this._commandId),
      ...(req.signal ? { signal: req.signal } : {}),
    });
  }
}

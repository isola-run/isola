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

// Long-poll interval — must stay <= api-gateway's maximum:"25".
// Full chain (per CLAUDE.md): SDK 20s < gateway 25s < sidecar 30s
//                             < gateway WriteTimeout 45s < sidecar WriteTimeout 75s
const LONG_POLL_WAIT_SECONDS = 20;

export interface SpawnOptions {
  env?: Record<string, string>;
  cwd?: string;
  /** Server-side process kill deadline. */
  timeoutSeconds?: number;
  container?: string;
}

export interface RunOptions extends SpawnOptions {
  input?: string | Uint8Array;
  /** Client-side deadline for the wait/read phase; throws IsolaTimeoutError. */
  waitTimeoutMs?: number;
}

export interface WaitOptions {
  /** Client-side wait deadline; throws IsolaTimeoutError on expiry. */
  timeoutMs?: number;
  signal?: AbortSignal;
}

function spawnPayload(args: string[], opts: SpawnOptions | undefined): CreateCommandPayload {
  const payload: CreateCommandPayload = { args };
  if (opts?.env !== undefined) payload.env = opts.env;
  if (opts?.cwd !== undefined) payload.cwd = opts.cwd;
  if (opts?.timeoutSeconds !== undefined) payload.timeoutSeconds = opts.timeoutSeconds;
  return payload;
}

function statusParams(): { waitSeconds: number } {
  return { waitSeconds: LONG_POLL_WAIT_SECONDS };
}

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

export class Commands {
  /** @internal */
  readonly _api: HttpClient;
  /** @internal */
  readonly _sandboxId: string;

  constructor(api: HttpClient, sandboxId: string) {
    this._api = api;
    this._sandboxId = sandboxId;
  }

  async spawn(args: string[], opts: SpawnOptions = {}, req: RequestOptions = {}): Promise<Command> {
    if (!Array.isArray(args) || args.length === 0) {
      throw new Error("at least one argument (the command) is required");
    }
    const payload = spawnPayload(args, opts);
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

  async run(args: string[], opts: RunOptions = {}, req: RequestOptions = {}): Promise<CommandResult> {
    const cmd = await this.spawn(args, opts, req);

    if (opts.input !== undefined) {
      await cmd.writeStdin(opts.input, req);
      await cmd.closeStdin(req);
    }

    // Compose the user signal with a wait-phase deadline. If any of the
    // siblings reject, abort the others via an internal controller so
    // resources release promptly.
    const internalController = new AbortController();
    const userSignal = req.signal;
    const signals: AbortSignal[] = [internalController.signal];
    if (userSignal) signals.push(userSignal);
    const composedSignal = signals.length === 1 ? signals[0] : AbortSignal.any(signals);

    try {
      const [stdout, stderr, exitCode] = await Promise.all([
        cmd.stdout.read(composedSignal ? { signal: composedSignal } : {}),
        cmd.stderr.read(composedSignal ? { signal: composedSignal } : {}),
        cmd.wait({
          ...(opts.waitTimeoutMs !== undefined ? { timeoutMs: opts.waitTimeoutMs } : {}),
          ...(composedSignal ? { signal: composedSignal } : {}),
        }),
      ]);
      return { id: cmd.id, stdout, stderr, exitCode };
    } catch (err) {
      // Cancel siblings as soon as one rejects so the other reads/wait don't
      // hang on a server that has stopped sending events.
      if (!internalController.signal.aborted) {
        internalController.abort(err instanceof Error ? err : new Error(String(err)));
      }
      throw err;
    }
  }
}

export class Command {
  /** @internal */
  readonly _api: HttpClient;
  /** @internal */
  readonly _sandboxId: string;
  /** @internal */
  readonly _commandId: string;
  private _stdout: StreamReader | null = null;
  private _stderr: StreamReader | null = null;

  constructor(api: HttpClient, sandboxId: string, commandId: string) {
    this._api = api;
    this._sandboxId = sandboxId;
    this._commandId = commandId;
  }

  get id(): string {
    return this._commandId;
  }

  get stdout(): StreamReader {
    if (this._stdout === null) {
      this._stdout = new StreamReader(this._api, commandStdoutPath(this._sandboxId, this._commandId));
    }
    return this._stdout;
  }

  get stderr(): StreamReader {
    if (this._stderr === null) {
      this._stderr = new StreamReader(this._api, commandStderrPath(this._sandboxId, this._commandId));
    }
    return this._stderr;
  }

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

  async wait(opts: WaitOptions = {}): Promise<number> {
    const path = commandStatusPath(this._sandboxId, this._commandId);
    const params = statusParams();
    const { signal, timeoutSignal } = buildWaitSignal(opts.signal, opts.timeoutMs);
    while (true) {
      try {
        const status = await this._api.requestModel<CommandStatusResponse>(
          {
            method: "GET",
            path,
            params,
            ...(signal ? { signal } : {}),
          },
          CommandStatusResponseModel.fromWire,
        );
        if (status.exitCode !== null) return status.exitCode;
      } catch (err) {
        if (timeoutSignal?.aborted) {
          throw new IsolaTimeoutError(`command ${this._commandId} did not complete within ${opts.timeoutMs}ms`, {
            cause: err,
          });
        }
        if (opts.signal?.aborted) throw opts.signal.reason ?? err;
        throw err;
      }
    }
  }

  async writeStdin(data: string | Uint8Array, req: RequestOptions = {}): Promise<void> {
    const raw: Uint8Array = typeof data === "string" ? new TextEncoder().encode(data) : data;
    await this._api.requestNoContent({
      method: "POST",
      path: commandStdinPath(this._sandboxId, this._commandId),
      body: raw as unknown as BodyInit,
      bodyKind: "replayable",
      headers: { "content-type": "application/octet-stream" },
      ...(req.signal ? { signal: req.signal } : {}),
    });
  }

  async closeStdin(req: RequestOptions = {}): Promise<void> {
    await this._api.requestNoContent({
      method: "POST",
      path: commandStdinClosePath(this._sandboxId, this._commandId),
      ...(req.signal ? { signal: req.signal } : {}),
    });
  }

  async kill(req: RequestOptions = {}): Promise<void> {
    await this._api.requestNoContent({
      method: "DELETE",
      path: commandPath(this._sandboxId, this._commandId),
      ...(req.signal ? { signal: req.signal } : {}),
    });
  }
}

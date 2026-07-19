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

import { NotFoundError } from "../errors";
import { sleep } from "./http";

/** @internal */
export interface PollOptions<T> {
  /** Fetch the current state of the resource being polled. */
  poll: (signal: AbortSignal | undefined) => Promise<T>;
  /** Returns true once the resource has reached the desired state. */
  isDone: (value: T) => boolean;
  /** Throws if the resource has reached a failed/terminal state. */
  assertNotFailed: (value: T) => void;
  /** Delay between polls, in milliseconds. */
  intervalMs: number;
  /** Total client-side budget, in milliseconds. */
  maxWaitMs: number;
  signal: AbortSignal | undefined;
  /** Throws the resource-specific timeout error (optionally chaining `cause`). */
  onTimeout: (cause?: unknown) => never;
  /** Seeds `lastValue` so a first-poll 404 can still resolve to a value. */
  initialValue?: T;
  /**
   * Decides what a `NotFoundError` means. Return a value to finish
   * successfully (the resource was deleted after completing); return
   * `undefined` to keep treating the 404 as cache lag and retry within the
   * deadline. `lastValue` is the most recently observed value (the seed if no
   * poll has succeeded yet); `seenDuringWait` is true once a poll succeeded.
   */
  onNotFound?: (info: { lastValue: T | undefined; seenDuringWait: boolean }) => T | undefined;
}

/**
 * Poll an eventually-consistent resource until `isDone`, it fails, or the
 * deadline passes. A `NotFoundError` is treated as "not ready yet" and retried
 * within the deadline (cache lag on a just-created resource), so the deadline is
 * always enforced even on the not-found path.
 *
 * @internal
 */
export async function pollUntilDone<T>(opts: PollOptions<T>): Promise<T> {
  const deadline = performance.now() + opts.maxWaitMs;
  let lastValue: T | undefined = opts.initialValue;
  let seenDuringWait = false;
  while (true) {
    let value: T;
    try {
      value = await opts.poll(opts.signal);
    } catch (err) {
      if (err instanceof NotFoundError) {
        const resolved = opts.onNotFound?.({ lastValue, seenDuringWait });
        if (resolved !== undefined) return resolved;
        if (performance.now() >= deadline) opts.onTimeout(err);
        await sleep(opts.intervalMs, opts.signal);
        continue;
      }
      throw err;
    }

    lastValue = value;
    seenDuringWait = true;
    if (opts.isDone(value)) return value;
    opts.assertNotFailed(value);
    if (performance.now() >= deadline) opts.onTimeout();
    await sleep(opts.intervalMs, opts.signal);
  }
}

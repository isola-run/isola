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

// Direct tests for the internal/http.ts module helpers (sleep, isReplayableBody)
// that are not easily exercised via the Isola client surface.

import { describe, expect, it } from "vitest";
import { isReplayableBody, sleep } from "../src/internal/http";

describe("sleep abort semantics", () => {
  it("rejects synchronously when the supplied signal is already aborted", async () => {
    const reason = new Error("pre-aborted");
    const ctrl = new AbortController();
    ctrl.abort(reason);

    // Covers http.ts:81-83 (the pre-aborted short-circuit branch).
    await expect(sleep(1_000, ctrl.signal)).rejects.toBe(reason);
  });

  it("rejects with signal.reason when aborted while sleeping", async () => {
    const reason = new Error("aborted-while-sleeping");
    const ctrl = new AbortController();

    // Covers http.ts:85-87 (the onAbort listener path that clears the timer
    // and rejects with the abort reason).
    const promise = sleep(10_000, ctrl.signal);
    setTimeout(() => ctrl.abort(reason), 5);

    await expect(promise).rejects.toBe(reason);
  });

  it("resolves normally when no signal is supplied", async () => {
    await expect(sleep(1)).resolves.toBeUndefined();
  });

  it("resolves normally when signal is supplied but never fires", async () => {
    const ctrl = new AbortController();
    await expect(sleep(1, ctrl.signal)).resolves.toBeUndefined();
  });
});

describe("isReplayableBody classification", () => {
  it("null and undefined are replayable (no body)", () => {
    expect(isReplayableBody(null)).toBe(true);
    expect(isReplayableBody(undefined)).toBe(true);
  });

  it("string, Uint8Array, ArrayBuffer, Blob, URLSearchParams are replayable", () => {
    expect(isReplayableBody("hi")).toBe(true);
    expect(isReplayableBody(new Uint8Array([1, 2, 3]))).toBe(true);
    expect(isReplayableBody(new ArrayBuffer(4))).toBe(true);
    expect(isReplayableBody(new Blob(["x"]))).toBe(true);
    // Covers http.ts:105 — URLSearchParams classification.
    expect(isReplayableBody(new URLSearchParams("a=1"))).toBe(true);
  });

  it("ReadableStream is NOT replayable", () => {
    const stream = new ReadableStream<Uint8Array>({
      start(c) {
        c.enqueue(new TextEncoder().encode("x"));
        c.close();
      },
    });
    // Covers http.ts:106 — the final `return false` for unrecognised body
    // types.
    expect(isReplayableBody(stream)).toBe(false);
  });

  it("arbitrary objects are NOT replayable", () => {
    // Defensive: the type system permits BodyInit but at runtime we should
    // not classify {} as replayable.
    expect(isReplayableBody({ some: "object" })).toBe(false);
    expect(isReplayableBody(42)).toBe(false);
  });
});

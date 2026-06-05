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

import { describe, expect, it } from "vitest";
import { combineSignals } from "../src/internal/signal";

describe("combineSignals", () => {
  it("returns undefined when every input is undefined", () => {
    expect(combineSignals()).toBeUndefined();
    expect(combineSignals(undefined, undefined)).toBeUndefined();
  });

  it("returns the single present signal unchanged (ignoring undefined inputs)", () => {
    const signal = new AbortController().signal;
    expect(combineSignals(undefined, signal, undefined)).toBe(signal);
  });

  it("composes a new signal that aborts when any input aborts", () => {
    const a = new AbortController();
    const b = new AbortController();
    const composed = combineSignals(a.signal, b.signal);

    expect(composed).toBeDefined();
    expect(composed).not.toBe(a.signal);
    expect(composed?.aborted).toBe(false);

    const reason = new Error("boom");
    b.abort(reason);
    expect(composed?.aborted).toBe(true);
    expect(composed?.reason).toBe(reason);
  });

  it("is already aborted when a member was aborted before composing", () => {
    const a = new AbortController();
    a.abort(new Error("pre-aborted"));
    const composed = combineSignals(a.signal, new AbortController().signal);
    expect(composed?.aborted).toBe(true);
  });
});

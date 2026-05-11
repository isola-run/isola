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
import {
  buildUrl,
  commandPath,
  commandStdoutPath,
  filesystemPath,
  normalizeUrl,
  quoteSegment,
  sandboxPath,
  snapshotPath,
} from "../src/internal/url";

describe("normalizeUrl", () => {
  it("strips a single trailing slash", () => {
    expect(normalizeUrl("http://example.com/")).toBe("http://example.com");
  });

  it("strips multiple trailing slashes", () => {
    expect(normalizeUrl("http://example.com////")).toBe("http://example.com");
  });

  it("trims surrounding whitespace", () => {
    expect(normalizeUrl("  http://example.com/  ")).toBe("http://example.com");
  });

  it("throws TypeError when input is whitespace only", () => {
    // Mirrors src/internal/url.ts:22 — the empty-after-trim throw branch.
    expect(() => normalizeUrl("   ")).toThrow(TypeError);
    expect(() => normalizeUrl("   ")).toThrow(/ISOLA_URL/);
  });

  it("throws TypeError when input is just slashes", () => {
    // Slashes get stripped by the regex, leaving an empty string.
    expect(() => normalizeUrl("///")).toThrow(TypeError);
  });

  it("throws TypeError when input is the empty string", () => {
    expect(() => normalizeUrl("")).toThrow(TypeError);
  });
});

describe("quoteSegment", () => {
  it("escapes !'()* (which encodeURIComponent leaves alone)", () => {
    expect(quoteSegment("!'()*")).toBe("%21%27%28%29%2A");
  });

  it("escapes spaces and slashes", () => {
    expect(quoteSegment("a b/c")).toBe("a%20b%2Fc");
  });

  it("preserves unreserved characters", () => {
    expect(quoteSegment("abc-DEF_123.~")).toBe("abc-DEF_123.~");
  });
});

describe("path helpers", () => {
  it("sandboxPath URL-encodes the id", () => {
    expect(sandboxPath("a b")).toBe("/v1/sandboxes/a%20b");
  });

  it("commandPath nests commandId under sandbox", () => {
    expect(commandPath("sb-1", "cmd-2")).toBe("/v1/sandboxes/sb-1/commands/cmd-2");
  });

  it("commandStdoutPath suffixes /stdout", () => {
    expect(commandStdoutPath("sb-1", "cmd-2")).toBe("/v1/sandboxes/sb-1/commands/cmd-2/stdout");
  });

  it("filesystemPath suffixes /filesystem", () => {
    expect(filesystemPath("sb-1")).toBe("/v1/sandboxes/sb-1/filesystem");
  });

  it("snapshotPath URL-encodes the id", () => {
    expect(snapshotPath("snap a")).toBe("/v1/rootfs-snapshots/snap%20a");
  });
});

describe("buildUrl", () => {
  it("returns base+path with no params", () => {
    expect(buildUrl("https://api.example.com", "/v1/x")).toBe("https://api.example.com/v1/x");
  });

  it("appends a non-empty query string", () => {
    expect(buildUrl("https://h", "/p", { a: "1", b: 2 })).toBe("https://h/p?a=1&b=2");
  });

  it("drops undefined and null params (no trailing ?)", () => {
    expect(buildUrl("https://h", "/p", { a: undefined, b: undefined })).toBe("https://h/p");
  });

  it("URL-encodes param values with `+` for spaces (matches Python httpx)", () => {
    expect(buildUrl("https://h", "/p", { q: "hello world" })).toBe("https://h/p?q=hello+world");
  });

  it("URL-encodes !'()* in param values (matches Python httpx)", () => {
    expect(buildUrl("https://h", "/p", { q: "a!b'c(d)e*f" })).toBe("https://h/p?q=a%21b%27c%28d%29e%2Af");
  });

  it("URL-encodes literal `+` as %2B in param values", () => {
    expect(buildUrl("https://h", "/p", { q: "a+b" })).toBe("https://h/p?q=a%2Bb");
  });

  it("URL-encodes paths with `+` for spaces in query params (matches Python httpx)", () => {
    expect(buildUrl("https://h", "/p", { path: "/foo bar/baz" })).toBe("https://h/p?path=%2Ffoo+bar%2Fbaz");
  });
});

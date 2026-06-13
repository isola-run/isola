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

// URL helpers. Mirrors the URL normalization and per-resource path helpers in
// the Python SDK (_client.py and the per-resource modules).

export function normalizeUrl(url: string): string {
  const trimmed = url.trim().replace(/\/+$/, "");
  if (trimmed === "") {
    throw new TypeError("url must be provided either as argument or via the ISOLA_URL environment variable");
  }
  return trimmed;
}

// Path-segment encoding matching Python's urllib.parse.quote(s, safe=''):
// escapes every non-unreserved character. encodeURIComponent leaves !, ', (, ),
// * unescaped; quote does not, so add them explicitly. encodeURIComponent
// throws URIError on lone/unpaired surrogates; surface that as a typed SDK
// error so users don't get an opaque URIError from deep inside fetch.
export function quoteSegment(s: string): string {
  try {
    return encodeURIComponent(s).replace(/[!'()*]/g, (c) => `%${c.charCodeAt(0).toString(16).toUpperCase()}`);
  } catch (err) {
    if (err instanceof URIError) {
      throw new TypeError(`invalid characters in URL component: ${err.message}`, { cause: err });
    }
    throw err;
  }
}

// Query-component encoding matching Python httpx's urlencode behavior: spaces
// become `+` (form-urlencoded style), and the additional reserved characters
// `!'()*` get percent-encoded like quoteSegment.
function quoteQueryComponent(s: string): string {
  return quoteSegment(s).replace(/%20/g, "+");
}

export function sandboxesPath(): string {
  return "/v1/sandboxes";
}

export function sandboxPath(sandboxId: string): string {
  return `/v1/sandboxes/${quoteSegment(sandboxId)}`;
}

export function rootfsSnapshotsPath(): string {
  return "/v1/rootfs-snapshots";
}

export function snapshotPath(snapshotId: string): string {
  return `/v1/rootfs-snapshots/${quoteSegment(snapshotId)}`;
}

export function commandBasePath(sandboxId: string): string {
  return `${sandboxPath(sandboxId)}/commands`;
}

export function commandPath(sandboxId: string, commandId: string): string {
  return `${commandBasePath(sandboxId)}/${quoteSegment(commandId)}`;
}

export function commandStatusPath(sandboxId: string, commandId: string): string {
  return `${commandPath(sandboxId, commandId)}/status`;
}

export function commandStdoutPath(sandboxId: string, commandId: string): string {
  return `${commandPath(sandboxId, commandId)}/stdout`;
}

export function commandStderrPath(sandboxId: string, commandId: string): string {
  return `${commandPath(sandboxId, commandId)}/stderr`;
}

export function commandStdinPath(sandboxId: string, commandId: string): string {
  return `${commandPath(sandboxId, commandId)}/stdin`;
}

export function commandStdinClosePath(sandboxId: string, commandId: string): string {
  return `${commandPath(sandboxId, commandId)}/stdin/close`;
}

export function filesystemPath(sandboxId: string): string {
  return `${sandboxPath(sandboxId)}/filesystem`;
}

export function filesystemEntriesPath(sandboxId: string): string {
  return `${filesystemPath(sandboxId)}/entries`;
}

export function filesystemStatPath(sandboxId: string): string {
  return `${filesystemPath(sandboxId)}/stat`;
}

// Builds a URL with optional query string. Drops undefined/null values.
// Uses quoteQueryComponent so spaces become `+` and !'()* are percent-encoded,
// matching Python httpx's QueryParams.__str__ wire output.
export function buildUrl(base: string, path: string, params?: Record<string, string | number | undefined>): string {
  let url = `${base}${path}`;
  if (!params) return url;
  const parts: string[] = [];
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null) continue;
    parts.push(`${quoteQueryComponent(key)}=${quoteQueryComponent(String(value))}`);
  }
  if (parts.length > 0) url += `?${parts.join("&")}`;
  return url;
}

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

// URL helpers — matches the path construction logic in Python _client.py:285-289
// and the per-resource path helpers across _sandbox.py / _commands.py /
// _filesystem.py / _rootfs_snapshot.py.

export function normalizeUrl(url: string): string {
  const trimmed = url.trim().replace(/\/+$/, "");
  if (trimmed === "") {
    throw new TypeError("url must be provided either as argument or via the ISOLA_URL environment variable");
  }
  return trimmed;
}

// Mirrors Python's urllib.parse.quote(s, safe=''): escapes EVERY non-unreserved
// character. encodeURIComponent leaves !, ', (, ), * unescaped — quote does not.
export function quoteSegment(s: string): string {
  return encodeURIComponent(s).replace(/[!'()*]/g, (c) => `%${c.charCodeAt(0).toString(16).toUpperCase()}`);
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

// Builds a URL with optional query string. Drops null/undefined/empty values.
export function buildUrl(base: string, path: string, params?: Record<string, string | number | undefined>): string {
  let url = `${base}${path}`;
  if (!params) return url;
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null) continue;
    search.append(key, String(value));
  }
  const qs = search.toString();
  if (qs.length > 0) url += `?${qs}`;
  return url;
}

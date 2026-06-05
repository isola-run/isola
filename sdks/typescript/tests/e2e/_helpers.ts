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

// Mirrors sdks/python/tests/e2e/utils.py.

import { type Isola, NotFoundError, type Sandbox, type SandboxStatus } from "../../src";

export const ISOLA_URL = process.env.ISOLA_URL ?? "http://localhost:8080";
export const ISOLA_METRICS_URL = process.env.ISOLA_METRICS_URL ?? "http://localhost:8082";

export const POLL_INTERVAL_MS = 1_000;
export const POLL_TIMEOUT_MS = 90_000;

export async function waitForRunning(client: Isola, sandboxId: string, timeoutMs = POLL_TIMEOUT_MS): Promise<Sandbox> {
  const deadline = performance.now() + timeoutMs;
  let lastStatus: SandboxStatus | undefined;
  while (performance.now() < deadline) {
    try {
      const sb = await client.sandboxes.get(sandboxId);
      lastStatus = sb.status;
      if (sb.status === "Running") return sb;
      if (sb.status === "Failed" || sb.status === "Succeeded") {
        throw new Error(`Sandbox ${sandboxId} reached terminal status: ${sb.status}`);
      }
    } catch (err) {
      if (!(err instanceof NotFoundError)) throw err;
    }
    await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
  }
  throw new Error(
    `Sandbox ${sandboxId} did not reach Running within ${timeoutMs}ms (last: ${lastStatus ?? "unknown"})`,
  );
}

export async function waitForVisible(client: Isola, sandboxId: string, timeoutMs = POLL_TIMEOUT_MS): Promise<Sandbox> {
  const deadline = performance.now() + timeoutMs;
  while (performance.now() < deadline) {
    try {
      return await client.sandboxes.get(sandboxId);
    } catch (err) {
      if (!(err instanceof NotFoundError)) throw err;
    }
    await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
  }
  throw new Error(`Sandbox ${sandboxId} not visible after ${timeoutMs}ms`);
}

export async function waitForStatus(
  client: Isola,
  sandboxId: string,
  desired: SandboxStatus,
  timeoutMs = POLL_TIMEOUT_MS,
): Promise<Sandbox> {
  const deadline = performance.now() + timeoutMs;
  while (performance.now() < deadline) {
    try {
      const sb = await client.sandboxes.get(sandboxId);
      if (sb.status === desired) return sb;
    } catch (err) {
      if (!(err instanceof NotFoundError)) throw err;
    }
    await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
  }
  throw new Error(`Sandbox ${sandboxId} did not reach ${desired} within ${timeoutMs}ms`);
}

export async function safeDelete(client: Isola, sandboxId: string): Promise<void> {
  try {
    const sb = await client.sandboxes.get(sandboxId);
    await sb.delete();
  } catch (err) {
    if (!(err instanceof NotFoundError)) console.warn(`failed to delete sandbox ${sandboxId}:`, err);
  }
}

// Mirrors Python tests/e2e/utils.py:parse_k8s_quantity. Used for asserting
// resource conversions on Container responses.
export function parseK8sQuantity(s: string): number {
  const match = /^(\d+(?:\.\d+)?)([a-zA-Z]*)$/.exec(s);
  if (!match) throw new Error(`invalid k8s quantity: ${s}`);
  const num = Number.parseFloat(match[1] ?? "0");
  const unit = match[2] ?? "";
  const multipliers: Record<string, number> = {
    "": 1,
    m: 1 / 1000,
    Ki: 1024,
    Mi: 1024 ** 2,
    Gi: 1024 ** 3,
    Ti: 1024 ** 4,
    K: 1000,
    M: 1000 ** 2,
    G: 1000 ** 3,
    T: 1000 ** 4,
  };
  const mult = multipliers[unit];
  if (mult === undefined) throw new Error(`unknown k8s unit: ${unit}`);
  return num * mult;
}

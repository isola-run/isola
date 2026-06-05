// CJS smoke test consumed by Node CI matrix (per spec F16: exercise both formats).
// Mirrors runtime-smoke.ts (ESM) but loads dist/index.cjs via require().
// Bun and Deno only run the ESM smoke; this file is Node-only.

const { APIError, BadRequestError, Isola, IsolaError, NotFoundError, VERSION } = require("../dist/index.cjs");

const errors = [];

if (typeof VERSION !== "string" || VERSION.length === 0) {
  errors.push("VERSION not exported as a non-empty string");
}

const e = new NotFoundError({ statusCode: 404, message: "not here" });
if (e.name !== "NotFoundError") errors.push(`error name = ${e.name} (want NotFoundError)`);
if (!(e instanceof IsolaError)) errors.push("NotFoundError not instanceof IsolaError");
if (!(e instanceof APIError)) errors.push("NotFoundError not instanceof APIError");

const e2 = new BadRequestError({ statusCode: 400, message: "bad" });
if (e2.statusCode !== 400) errors.push(`statusCode = ${e2.statusCode} (want 400)`);
if (!e2.message.startsWith("400: ")) errors.push("BadRequestError message missing prefix");

// Constructor validation: requestTimeoutMs must be a positive number, null, or undefined.
try {
  new Isola({ url: "http://localhost:8080", requestTimeoutMs: -1 });
  errors.push("Isola constructor accepted negative requestTimeoutMs");
} catch (err) {
  if (!(err instanceof TypeError)) errors.push(`expected TypeError, got ${err?.constructor?.name}`);
}

// Constructor accepts null to disable timeouts.
const ok = new Isola({ url: "http://localhost:8080", requestTimeoutMs: null });
if (!ok.url.endsWith(":8080")) errors.push(`url normalisation broken: ${ok.url}`);

if (errors.length > 0) {
  for (const err of errors) console.error("FAIL:", err);
  throw new Error(`${errors.length} smoke check(s) failed`);
}

console.log("runtime-smoke (cjs): ok");

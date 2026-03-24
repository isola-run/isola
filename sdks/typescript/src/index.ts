// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------
export { Isola, type IsolaOptions } from "./isola.js";

// ---------------------------------------------------------------------------
// Resources
// ---------------------------------------------------------------------------
export { Sandboxes, Sandbox, type CreateSandboxOptions } from "./sandbox.js";
export {
  Commands,
  Command,
  type SpawnOptions,
  type RunOptions,
} from "./commands.js";
export { Filesystem } from "./filesystem.js";
export { StreamReader } from "./streaming.js";

// ---------------------------------------------------------------------------
// Models / types
// ---------------------------------------------------------------------------
export type {
  SandboxStatus,
  NetworkSpec,
  RootfsSnapshotSource,
  ResourceList,
  ResourcesSpec,
  ContainerSpec,
  ContainerInfo,
  PodTemplate,
  PodTemplateInfo,
  SandboxSummary,
  SandboxData,
  CommandResult,
  FileWriteResult,
} from "./models.js";

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------
export {
  IsolaError,
  APIError,
  BadRequestError,
  NotFoundError,
  ConflictError,
  ValidationError,
  InternalError,
  BadGatewayError,
  APIConnectionError,
  isTransient,
} from "./errors.js";

// ---------------------------------------------------------------------------
// Version
// ---------------------------------------------------------------------------
export { VERSION } from "./version.js";

import type {
  CreateSandboxRequest,
  SandboxResponse,
  ListSandboxesResponse,
  CreateCommandRequest,
  CreateCommandResponse,
  CommandStatusResponse,
  CreateRootfsSnapshotRequest,
  RootfsSnapshotResponse,
  FilesystemWriteResponse,
  ApiError,
} from "../types";

function enc(id: string): string {
  return encodeURIComponent(id);
}

class ApiClient {
  private baseUrl: string;

  constructor(baseUrl = "") {
    this.baseUrl = baseUrl;
  }

  private async request<T>(
    path: string,
    options: RequestInit = {}
  ): Promise<T> {
    const { headers: customHeaders, ...rest } = options;
    const res = await fetch(`${this.baseUrl}${path}`, {
      ...rest,
      headers: { "Content-Type": "application/json", ...customHeaders },
    });
    if (!res.ok) {
      const err: ApiError = await res.json().catch(() => ({
        status: res.status,
        title: res.statusText,
      }));
      throw err;
    }
    if (res.status === 204) return undefined as T;
    return res.json();
  }

  // Sandboxes
  listSandboxes(): Promise<ListSandboxesResponse> {
    return this.request("/v1/sandboxes");
  }

  getSandbox(id: string): Promise<SandboxResponse> {
    return this.request(`/v1/sandboxes/${enc(id)}`);
  }

  createSandbox(req: CreateSandboxRequest): Promise<SandboxResponse> {
    return this.request("/v1/sandboxes", {
      method: "POST",
      body: JSON.stringify(req),
    });
  }

  deleteSandbox(id: string): Promise<void> {
    return this.request(`/v1/sandboxes/${enc(id)}`, { method: "DELETE" });
  }

  // Commands
  createCommand(
    sandboxId: string,
    req: CreateCommandRequest,
    container?: string
  ): Promise<CreateCommandResponse> {
    const qs = container ? `?container=${enc(container)}` : "";
    return this.request(`/v1/sandboxes/${enc(sandboxId)}/commands${qs}`, {
      method: "POST",
      body: JSON.stringify(req),
    });
  }

  getCommandStatus(
    sandboxId: string,
    commandId: string,
    waitSeconds = 0
  ): Promise<CommandStatusResponse> {
    const qs = waitSeconds > 0 ? `?waitSeconds=${waitSeconds}` : "";
    return this.request(
      `/v1/sandboxes/${enc(sandboxId)}/commands/${enc(commandId)}/status${qs}`
    );
  }

  deleteCommand(sandboxId: string, commandId: string): Promise<void> {
    return this.request(
      `/v1/sandboxes/${enc(sandboxId)}/commands/${enc(commandId)}`,
      { method: "DELETE" }
    );
  }

  streamStdout(
    sandboxId: string,
    commandId: string,
    onData: (text: string) => void,
    onDone: () => void
  ): () => void {
    return this.streamSSE(
      `/v1/sandboxes/${enc(sandboxId)}/commands/${enc(commandId)}/stdout`,
      onData,
      onDone
    );
  }

  streamStderr(
    sandboxId: string,
    commandId: string,
    onData: (text: string) => void,
    onDone: () => void
  ): () => void {
    return this.streamSSE(
      `/v1/sandboxes/${enc(sandboxId)}/commands/${enc(commandId)}/stderr`,
      onData,
      onDone
    );
  }

  private streamSSE(
    path: string,
    onData: (text: string) => void,
    onDone: () => void
  ): () => void {
    const eventSource = new EventSource(`${this.baseUrl}${path}`);
    eventSource.onmessage = (event) => {
      try {
        const decoded = atob(event.data);
        onData(decoded);
      } catch {
        onData(event.data);
      }
    };
    eventSource.onerror = () => {
      eventSource.close();
      onDone();
    };
    return () => eventSource.close();
  }

  writeStdin(
    sandboxId: string,
    commandId: string,
    data: string
  ): Promise<void> {
    return this.request(
      `/v1/sandboxes/${enc(sandboxId)}/commands/${enc(commandId)}/stdin`,
      {
        method: "POST",
        headers: { "Content-Type": "application/octet-stream" },
        body: data,
      }
    );
  }

  closeStdin(sandboxId: string, commandId: string): Promise<void> {
    return this.request(
      `/v1/sandboxes/${enc(sandboxId)}/commands/${enc(commandId)}/stdin/close`,
      { method: "POST" }
    );
  }

  // Filesystem
  async readFile(sandboxId: string, path: string, container?: string): Promise<Blob> {
    const params = new URLSearchParams({ path });
    if (container) params.set("container", container);
    const res = await fetch(
      `${this.baseUrl}/v1/sandboxes/${enc(sandboxId)}/filesystem?${params}`
    );
    if (!res.ok) {
      const err: ApiError = await res.json().catch(() => ({
        status: res.status,
        title: res.statusText,
      }));
      throw err;
    }
    return res.blob();
  }

  async writeFile(
    sandboxId: string,
    path: string,
    data: Blob | ArrayBuffer | string,
    container?: string
  ): Promise<FilesystemWriteResponse> {
    const params = new URLSearchParams({ path });
    if (container) params.set("container", container);
    const res = await fetch(
      `${this.baseUrl}/v1/sandboxes/${enc(sandboxId)}/filesystem?${params}`,
      {
        method: "POST",
        headers: { "Content-Type": "application/octet-stream" },
        body: data,
      }
    );
    if (!res.ok) {
      const err: ApiError = await res.json().catch(() => ({
        status: res.status,
        title: res.statusText,
      }));
      throw err;
    }
    return res.json();
  }

  // Snapshots
  createSnapshot(
    req: CreateRootfsSnapshotRequest
  ): Promise<RootfsSnapshotResponse> {
    return this.request("/v1/rootfs-snapshots", {
      method: "POST",
      body: JSON.stringify(req),
    });
  }

  getSnapshot(id: string): Promise<RootfsSnapshotResponse> {
    return this.request(`/v1/rootfs-snapshots/${enc(id)}`);
  }
}

export const api = new ApiClient();

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

class ApiClient {
  private baseUrl: string;

  constructor(baseUrl = "") {
    this.baseUrl = baseUrl;
  }

  private async request<T>(
    path: string,
    options: RequestInit = {}
  ): Promise<T> {
    const res = await fetch(`${this.baseUrl}${path}`, {
      headers: { "Content-Type": "application/json", ...options.headers },
      ...options,
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
    return this.request(`/v1/sandboxes/${id}`);
  }

  createSandbox(req: CreateSandboxRequest): Promise<SandboxResponse> {
    return this.request("/v1/sandboxes", {
      method: "POST",
      body: JSON.stringify(req),
    });
  }

  deleteSandbox(id: string): Promise<void> {
    return this.request(`/v1/sandboxes/${id}`, { method: "DELETE" });
  }

  // Commands
  createCommand(
    sandboxId: string,
    req: CreateCommandRequest
  ): Promise<CreateCommandResponse> {
    return this.request(`/v1/sandboxes/${sandboxId}/commands`, {
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
      `/v1/sandboxes/${sandboxId}/commands/${commandId}/status${qs}`
    );
  }

  deleteCommand(sandboxId: string, commandId: string): Promise<void> {
    return this.request(
      `/v1/sandboxes/${sandboxId}/commands/${commandId}`,
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
      `/v1/sandboxes/${sandboxId}/commands/${commandId}/stdout`,
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
      `/v1/sandboxes/${sandboxId}/commands/${commandId}/stderr`,
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
      `/v1/sandboxes/${sandboxId}/commands/${commandId}/stdin`,
      {
        method: "POST",
        headers: { "Content-Type": "application/octet-stream" },
        body: data,
      }
    );
  }

  closeStdin(sandboxId: string, commandId: string): Promise<void> {
    return this.request(
      `/v1/sandboxes/${sandboxId}/commands/${commandId}/stdin/close`,
      { method: "POST" }
    );
  }

  // Filesystem
  async readFile(sandboxId: string, path: string): Promise<Blob> {
    const res = await fetch(
      `${this.baseUrl}/v1/sandboxes/${sandboxId}/filesystem?path=${encodeURIComponent(path)}`
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
    data: Blob | ArrayBuffer | string
  ): Promise<FilesystemWriteResponse> {
    const res = await fetch(
      `${this.baseUrl}/v1/sandboxes/${sandboxId}/filesystem?path=${encodeURIComponent(path)}`,
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
    return this.request(`/v1/rootfs-snapshots/${id}`);
  }
}

export const api = new ApiClient();

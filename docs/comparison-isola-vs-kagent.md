# Isola vs kagent: Comparison

## Executive Summary

**Isola** and **kagent** are both Kubernetes-native projects, but they solve fundamentally different problems:

- **Isola** is a **sandbox orchestration platform** — it creates, manages, and provides programmatic access to isolated container environments (sandboxes) for code execution, AI agent compute, and secure workloads.
- **kagent** is an **AI agent framework** — it provides the infrastructure to build, deploy, and run LLM-powered AI agents that can interact with Kubernetes and cloud-native systems.

In short: **Isola provides the compute environment; kagent provides the AI brain.** They are complementary, not competitive — a kagent agent could use Isola sandboxes as its execution environment.

---

## At a Glance

| Dimension | Isola | kagent |
|---|---|---|
| **Purpose** | Secure sandbox orchestration | AI agent framework for K8s |
| **Core question it answers** | "Where does code run safely?" | "How do AI agents operate on K8s?" |
| **Primary user** | Platform teams, AI infra builders | DevOps/SRE teams, AI agent builders |
| **CNCF status** | Not a CNCF project | CNCF Sandbox (accepted May 2025) |
| **Origin** | isola-ai | Solo.io |
| **Language** | Go (operator, gateway, sidecar) + Python SDK | Go (~48%, controller + Go ADK) + Python (~22%, engine/ADK) + TypeScript (~24%, UI) |
| **K8s integration** | Operator + CRDs (Sandbox, RootfsSnapshot) | Operator + CRDs (Agent, ModelConfig, RemoteMCPServer, MCPServer) |
| **LLM involvement** | None — LLM-agnostic compute | Core — agents are LLM-powered |
| **Agent-agent protocol** | N/A | A2A (Agent-to-Agent, JSON-RPC 2.0 over SSE) |
| **Tool protocol** | N/A | MCP (Model Context Protocol) |
| **GitHub stars** | — | ~2,400+ |
| **License** | — | Apache 2.0 |

---

## Architecture Comparison

### Isola Architecture

```
User / SDK
    │
    ▼
API Gateway (REST, Huma+chi)
    │
    ├──► K8s API (Sandbox CRUD)
    │       │
    │       ▼
    │    Operator (controller-runtime)
    │       ├── Pod lifecycle
    │       ├── Network policies
    │       ├── Timeout enforcement
    │       └── Snapshot jobs
    │
    └──► Sandbox Pod
            ├── User container(s)
            └── Sidecar (command exec, file I/O, SSE streaming)
```

**Key components:** 4 binaries — operator, api-gateway, sandbox-sidecar, uploader.

The architecture is designed around **isolation and control**: every sandbox runs in its own pod with deny-all network policies by default, commands execute via chroot into `/proc/<pid>/root`, and the sidecar handles all I/O streaming.

### kagent Architecture

```
User (Next.js UI / CLI)
    │
    ▼ A2A JSON-RPC over SSE
Controller Manager (Go, port 8083)
    │
    ├── Watches Agent, ModelConfig, RemoteMCPServer CRDs
    ├── Reconciles → Deployment + Service + Secret + ServiceAccount per agent
    ├── Proxies A2A messages to agent pods
    │
    ▼ A2A JSON-RPC
Agent Pod (one per agent)
    │
    ├── ADK Runtime (Python or Go)
    │   ├── LLM interaction loop (prompt → LLM → tool call → result)
    │   ├── Reads config from mounted Secret (config.json)
    │   └── A2A server for subagent invocation
    │
    └──► MCP Tool Servers
         ├── K8s, Istio, Helm, Argo
         ├── Prometheus, Grafana, Cilium
         └── Custom MCP servers
```

**Key components:** Controller Manager (Go), Agent Runtime/Engine (Python ADK or Go ADK), Web UI (Next.js), CLI (Go).

**Data flow:** UI sends A2A JSON-RPC → Controller looks up agent pod Service (`http://{agent-name}.{namespace}:8080`) → Agent pod's ADK executor builds LLM request (system prompt + history + tools) → LLM responds with text or tool calls → MCP tool invocation if needed → Results stream back as SSE events → UI renders.

**Each agent gets its own pod**, Service, ServiceAccount, and config Secret. The controller acts as a proxy between clients and agent pods. State is persisted in SQLite or PostgreSQL (performance cache for UI queries; CRDs remain source of truth).

---

## What Each Project Does Well

### Isola Strengths

| Capability | Details |
|---|---|
| **Sandbox isolation** | Deny-all egress by default, per-sandbox NetworkPolicy, configurable CIDR allowlists, sink DNS |
| **Command execution** | Non-blocking spawn with SSE streaming, stdin/stdout/stderr, long-polling, per-command timeouts |
| **Filesystem access** | Upload/download files to/from sandbox containers |
| **Rootfs snapshots** | Checkpoint/restore overlay filesystem via gVisor, cloud storage (S3/GCS/Azure) |
| **Python SDK** | Full sync/async client with auto-reconnect streaming, Pydantic models, retry logic |
| **REST API** | Clean `/v1/` API with OpenAPI specs, streaming support, idempotent deletes |
| **Timeout chain** | Carefully tuned SDK → gateway → sidecar timeout cascade |
| **Security model** | gVisor runtime support, network isolation, env var write-only (no secret leakage) |

### kagent Strengths

| Capability | Details |
|---|---|
| **Declarative agents** | Define AI agents as K8s CRDs (system prompt, tools, LLM config, sub-agents) |
| **Multi-LLM support** | OpenAI, Anthropic, Azure OpenAI, Google Vertex AI/Gemini, Ollama, AWS Bedrock, custom gateways |
| **Built-in tool catalog** | MCP servers for K8s, Istio, Helm, Argo, Prometheus, Grafana, Cilium |
| **Multi-agent orchestration** | A2A (Agent-to-Agent) protocol for agent teams, planning agents, task delegation |
| **Human-in-the-loop** | Tool approval workflows, nested HITL through agent chains |
| **Observability** | OpenTelemetry tracing for agent and tool execution |
| **Web UI** | Built-in management interface for agents and tools |
| **Agent mesh vision** | kagent + agentgateway + Istio for enterprise agent networking |
| **Prompt templates** | Composable system prompts with `{{include "source/key"}}` from ConfigMaps and variable interpolation |
| **Cross-namespace sharing** | RemoteMCPServer supports `AllowedNamespaces` selectors for controlled tool sharing |
| **BYO agents** | Support for custom container images alongside declarative agents |

---

## CRD Comparison

### Isola CRDs

| CRD | Purpose |
|---|---|
| **Sandbox** | A running sandbox instance — pod template, network config, timeout, shutdown policy, snapshot sources |
| **RootfsSnapshot** | Triggers a point-in-time snapshot of a sandbox's overlay rootfs for checkpoint/restore |

Isola's CRDs are **infrastructure-focused**: they define what runs, how it's isolated, and how state is preserved.

### kagent CRDs (`kagent.dev/v1alpha2`)

| CRD | Purpose |
|---|---|
| **Agent** | An AI agent — two modes: *Declarative* (system prompt, model config ref, tool refs, deployment spec, memory config, prompt templates) or *BYO* (custom container image). CEL-validated. |
| **ModelConfig** | LLM provider configuration — provider-specific sub-configs (Anthropic, OpenAI, Azure OpenAI, Ollama, Gemini, Bedrock). API keys via K8s Secrets. |
| **RemoteMCPServer** | External MCP tool server declaration. Controller connects, discovers tools, stores in DB. Supports namespace-scoped access control via `AllowedNamespaces`. |
| **MCPServer** | Managed MCP servers via the KMCP operator (separate sub-project). |

kagent's CRDs are **agent-focused**: they define what an agent knows, what it can do, and which LLM powers it. Three controllers (Agent, RemoteMCPServer, MCPServer) share a single reconciler with database-level concurrency control.

---

## Key Differences

### 1. Core Abstraction

- **Isola**: The core abstraction is a **Sandbox** — an isolated, programmable container environment. Users create sandboxes, execute commands in them, stream output, and manage files. The sandbox itself has no intelligence.
- **kagent**: The core abstraction is an **Agent** — an LLM-powered entity with a system prompt, tools, and the ability to reason and act. The agent has intelligence but needs external systems to act upon.

### 2. Execution Model

- **Isola**: Commands run inside sandboxes via chroot. The sidecar manages process lifecycle, stdin/stdout/stderr streaming (SSE), and timeout enforcement. All execution is explicit — the user (or their code) decides what to run.
- **kagent**: The LLM decides what tools to call based on the user's request. The ADK engine orchestrates the agent loop (prompt → LLM → tool call → result → LLM → ...). Execution is autonomous within the agent's tool scope.

### 3. Network & Security Model

- **Isola**: Deep security isolation — deny-all egress, per-sandbox NetworkPolicy, gVisor runtime, CIDR allowlists, blocked private ranges, sink DNS. Designed for running untrusted code.
- **kagent**: Security is about agent permissions — which tools an agent can access, human-in-the-loop approval for sensitive operations, and user identity propagation. Not designed for untrusted code execution.

### 4. State & Persistence

- **Isola**: Sandbox state is ephemeral (in-memory command state, lost on sidecar restart). Rootfs snapshots provide checkpoint/restore for the filesystem layer via cloud storage.
- **kagent**: Agent state is persisted in a database (SQLite or PostgreSQL via GORM). Session history, conversation context, and tool results are stored for continuity and observability. CRDs remain the source of truth; the database is a performance cache.

### 5. SDK & API Surface

- **Isola**: REST API + Python SDK focused on sandbox lifecycle and I/O (create/delete sandbox, run commands, stream output, upload/download files).
- **kagent**: REST API + CLI + Web UI focused on agent management (create agents, configure tools, start conversations, view traces).

---

## Complementary Use Case

The two projects could work together naturally:

1. **kagent** defines an AI agent that needs to execute code safely
2. The agent uses an MCP tool that calls **Isola's API** to create a sandbox
3. The agent runs code in the Isola sandbox, streams output, and reads results
4. The sandbox provides security isolation so the agent can't escape or damage the cluster
5. Isola snapshots preserve the sandbox state between agent sessions

This is the "AI brain + secure hands" pattern: kagent provides reasoning and planning; Isola provides safe execution.

---

## Technology Stack Comparison

| Layer | Isola | kagent |
|---|---|---|
| **Primary language** | Go | Go + Python |
| **K8s framework** | controller-runtime | controller-runtime |
| **API framework** | Huma + chi (Go) | HTTP server (Go, port 8083) |
| **Agent runtime** | N/A | Google ADK (Python or Go) |
| **LLM integration** | None | Multi-provider (OpenAI, Anthropic, Gemini, Bedrock, Ollama, etc.) |
| **Tool protocol** | N/A | MCP (Model Context Protocol) — Streamable HTTP and SSE transports |
| **Agent protocol** | N/A | A2A (Agent-to-Agent) — JSON-RPC 2.0 over SSE |
| **Client SDK** | Python (httpx, pydantic) | Go client SDK, Python engine |
| **Streaming** | SSE (text/event-stream) for command output | A2A task events over SSE |
| **Observability** | Prometheus metrics | OpenTelemetry tracing |
| **Storage** | gocloud.dev (S3/GCS/Azure) for snapshots | SQLite or PostgreSQL (GORM) for sessions |
| **Container runtime** | gVisor (optional) or default | Cluster default |
| **Deployment per workload** | 1 sidecar per sandbox pod | 1 pod + Service + Secret per agent |
| **Testing** | Ginkgo/Gomega + envtest + pytest | Go tests + e2e |
| **UI** | None (API/SDK only) | Next.js web UI |

---

## When to Use Which

| Scenario | Use |
|---|---|
| Run untrusted code in isolated containers | **Isola** |
| Build AI agents that operate on K8s infrastructure | **kagent** |
| Provide sandboxed compute for AI coding assistants | **Isola** |
| Create a chatbot that can query Prometheus and manage Helm releases | **kagent** |
| Checkpoint and restore container filesystem state | **Isola** |
| Orchestrate multi-agent teams with planning and delegation | **kagent** |
| Stream command stdout/stderr in real-time from containers | **Isola** |
| Define agents declaratively as Kubernetes resources | **kagent** |
| AI agent needs a safe place to execute generated code | **Isola** (as kagent's tool) |

---

## Summary

| | Isola | kagent |
|---|---|---|
| **One-liner** | Kubernetes-native sandbox orchestration for secure code execution | Kubernetes-native framework for building and running AI agents |
| **Layer** | Infrastructure (compute isolation) | Application (AI agent orchestration) |
| **Intelligence** | None — pure execution environment | LLM-powered reasoning and tool use |
| **Relationship** | Could be kagent's execution backend | Could be Isola's intelligent frontend |

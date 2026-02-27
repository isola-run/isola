---
sidebar_position: 1
slug: /introduction
title: Introduction
---

# Introduction

Isola is a secure sandbox orchestration platform for Kubernetes. It enables you to create, manage, and interact with isolated sandbox environments using Kubernetes-native resources.

## What is Isola?

Isola provides:

- **Sandbox lifecycle management** via Kubernetes Custom Resource Definitions (CRDs)
- **A REST API** for creating sandboxes, executing commands, and transferring files
- **A Python SDK** with both synchronous and asynchronous clients
- **gVisor-based isolation** for kernel-level security
- **Network isolation** with deny-all defaults and fine-grained egress controls
- **Filesystem snapshots** to capture and upload sandbox state to cloud storage

## Architecture at a Glance

Isola consists of four components:

| Component | Role |
|-----------|------|
| **Operator** | Kubernetes controller that manages Sandbox and RootfsSnapshot CRDs |
| **API Gateway** | REST API that exposes sandbox operations to external clients |
| **Sandbox Sidecar** | Injected into each sandbox pod to handle command execution and file I/O |
| **Uploader** | Job container that snapshots and uploads sandbox filesystems |

## How It Works

1. You create a **Sandbox** resource (via the API, SDK, or `kubectl`).
2. The **Operator** creates a pod with your container image plus the sidecar.
3. The **API Gateway** proxies commands and file operations to the sidecar.
4. When you're done, delete the sandbox or let it expire via `activeDeadlineSeconds`.

## License

Isola is licensed under the [Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0).

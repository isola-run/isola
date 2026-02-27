---
sidebar_position: 1
title: SDK Overview
slug: /sdk/overview
---

# Python SDK Overview

The `isola` Python package is a thin client for the Isola REST API. It provides both synchronous and asynchronous interfaces.

## Installation

```bash
pip install isola
```

**Requirements:** Python 3.10+

**Dependencies:** [httpx](https://www.python-httpx.org/) (HTTP client), [pydantic](https://docs.pydantic.dev/) (data models)

## Quick Example

### Synchronous

```python
from isola import Isola

with Isola(base_url="http://localhost:8080") as client:
    sandbox = client.sandboxes.create(image="python:3.12")

    cmd = sandbox.commands.run(cmd="python", args=["-c", "print('hello')"])

    with cmd.stdout() as stream:
        for chunk in stream:
            print(chunk, end="")

    sandbox.delete()
```

### Asynchronous

```python
from isola import AsyncIsola

async with AsyncIsola(base_url="http://localhost:8080") as client:
    sandbox = await client.sandboxes.create(image="python:3.12")

    cmd = await sandbox.commands.run(cmd="ls", args=["-la"])

    async with cmd.stdout() as stream:
        async for chunk in stream:
            print(chunk, end="")

    await sandbox.delete()
```

## Class Hierarchy

Every public class has a sync and async variant:

| Sync | Async | Purpose |
|------|-------|---------|
| `Isola` | `AsyncIsola` | Top-level client |
| `Sandbox` | `AsyncSandbox` | Sandbox instance |
| `Command` | `AsyncCommand` | Running command |
| `Filesystem` | `AsyncFilesystem` | File operations |
| `CommandOutputStream` | `AsyncCommandOutputStream` | Output streaming |

## Object Model

```
Isola(base_url="...")
  └── .sandboxes
        ├── .create(image=...) → Sandbox
        ├── .list() → list[SandboxSummary]
        └── .get(sandbox_id) → Sandbox

Sandbox
  ├── .id, .status, .creation_timestamp, .network, .active_deadline_seconds
  ├── .delete()
  ├── .commands
  │     └── .run(cmd=...) → Command
  └── .filesystem
        ├── .write(path, data) → FileWriteResult
        └── .read(path) → bytes

Command
  ├── .id
  ├── .stdout(offset=0, timeout=None) → CommandOutputStream
  ├── .stderr(offset=0, timeout=None) → CommandOutputStream
  ├── .exit_code() → int | None
  ├── .write_stdin(data) → None
  └── .kill() → None
```

## Context Managers

Both `Isola` and `AsyncIsola` are context managers that close the underlying HTTP client on exit:

```python
# Sync
with Isola(base_url="http://localhost:8080") as client:
    ...

# Async
async with AsyncIsola(base_url="http://localhost:8080") as client:
    ...

# Manual close
client = Isola(base_url="http://localhost:8080")
try:
    ...
finally:
    client.close()
```

## Timeout Configuration

The default HTTP timeout is 10 seconds with a 5 second connect timeout. Streaming connections have separate timeout settings — see [Streaming](./streaming.md).

---
sidebar_position: 4
title: Filesystem
---

# Filesystem

Isola supports reading and writing files inside sandbox containers through the API gateway.

## Writing Files

Upload a file to the sandbox:

```
POST /sandboxes/{id}/filesystem?path=/workspace/file.txt
Content-Type: application/octet-stream

<file bytes>
```

The response includes the absolute path where the file was written and the number of bytes:

```json
{
  "absolutePath": "/workspace/file.txt",
  "bytesWritten": 1024
}
```

### Parameters

| Parameter | Location | Required | Description |
|-----------|----------|----------|-------------|
| `path` | query | Yes | Destination path (absolute or relative to container cwd) |
| `container` | query | No | Container name (defaults to the only container) |

## Reading Files

Download a file from the sandbox:

```
GET /sandboxes/{id}/filesystem?path=/workspace/file.txt
```

Returns the file content as `application/octet-stream`.

### Parameters

| Parameter | Location | Required | Description |
|-----------|----------|----------|-------------|
| `path` | query | Yes | Source path (absolute or relative to container cwd) |
| `container` | query | No | Container name (defaults to the only container) |

## How It Works

File operations are proxied from the API gateway to the sandbox sidecar. The sidecar accesses the container's filesystem through `/proc/<pid>/root`, which provides direct access to the container's mount namespace without requiring `nsenter`.

## Using the Python SDK

```python
# Write a file
result = sandbox.filesystem.write("/workspace/hello.py", b"print('hello')")
print(f"Wrote {result.bytes_written} bytes to {result.absolute_path}")

# Read a file
content = sandbox.filesystem.read("/workspace/hello.py")
print(content.decode())
```

### Writing from a file object

```python
with open("local-file.tar.gz", "rb") as f:
    sandbox.filesystem.write("/workspace/archive.tar.gz", f)
```

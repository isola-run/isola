---
sidebar_position: 5
title: Filesystem
slug: /sdk/filesystem
---

# SDK Filesystem

The `Filesystem` class provides file read and write operations for sandbox containers.

## Writing Files

```python
result = sandbox.filesystem.write("/workspace/hello.py", b"print('hello')")

print(result.absolute_path)   # "/workspace/hello.py"
print(result.bytes_written)   # 14
```

### From a File Object

```python
with open("local-file.tar.gz", "rb") as f:
    result = sandbox.filesystem.write("/workspace/archive.tar.gz", f)
```

### Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `path` | str | Yes | Destination path (absolute or relative to container cwd) |
| `data` | bytes \| BinaryIO | Yes | File content |
| `container` | str | No | Target container name |

### Return Value

`FileWriteResult` with:

| Field | Type | Description |
|-------|------|-------------|
| `absolute_path` | str | Absolute path where the file was written |
| `bytes_written` | int | Number of bytes written |

## Reading Files

```python
content = sandbox.filesystem.read("/workspace/hello.py")
print(content.decode())  # "print('hello')"
```

### Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `path` | str | Yes | Source path (absolute or relative to container cwd) |
| `container` | str | No | Target container name |

### Return Value

`bytes` — the raw file content.

## Async Usage

```python
result = await sandbox.filesystem.write("/workspace/file.txt", b"content")
content = await sandbox.filesystem.read("/workspace/file.txt")
```

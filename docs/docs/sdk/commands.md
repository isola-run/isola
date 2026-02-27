---
sidebar_position: 4
title: Commands
slug: /sdk/commands
---

# SDK Commands

## Running Commands

```python
cmd = sandbox.commands.run(
    cmd="python",
    args=["-c", "print('hello')"],
    env={"MY_VAR": "override"},
    cwd="/workspace",
    timeout=30,
    container="main",      # Optional, for multi-container pods
    text=True,             # Text mode (default) or binary mode
)
```

### Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `cmd` | str | Yes | Executable path |
| `args` | list[str] | No | Arguments |
| `env` | dict[str, str] | No | Environment variable overrides |
| `cwd` | str | No | Working directory |
| `timeout` | int | No | Max execution time in seconds |
| `container` | str | No | Target container name |
| `text` | bool | No | Text mode (default: True). When True, output is UTF-8 decoded. When False, raw bytes are yielded. |

## Command Properties

| Property | Type | Description |
|----------|------|-------------|
| `id` | str | Command UUID |

## Reading Output

### Streaming stdout

```python
with cmd.stdout() as stream:
    for chunk in stream:
        print(chunk, end="")
```

### Streaming stderr

```python
with cmd.stderr() as stream:
    for chunk in stream:
        print(chunk, end="")
```

### Stream Options

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `offset` | int | 0 | Byte offset to resume from |
| `timeout` | float | None | Read timeout in seconds |

```python
# Resume from byte 1024
with cmd.stdout(offset=1024) as stream:
    for chunk in stream:
        print(chunk, end="")

# With a read timeout
with cmd.stdout(timeout=30.0) as stream:
    for chunk in stream:
        print(chunk, end="")
```

See [Streaming](./streaming.md) for details on reconnection behavior.

## Checking Exit Code

```python
exit_code = cmd.exit_code()

if exit_code is None:
    print("Command is still running")
elif exit_code == 0:
    print("Command succeeded")
else:
    print(f"Command failed with exit code {exit_code}")
```

## Writing to stdin

```python
cmd.write_stdin(b"some input data\n")
```

## Killing Commands

```python
cmd.kill()
```

This is idempotent — killing an already-exited command is a no-op.

## Async Usage

```python
cmd = await sandbox.commands.run(cmd="echo", args=["hello"])

async with cmd.stdout() as stream:
    async for chunk in stream:
        print(chunk, end="")

exit_code = await cmd.exit_code()
await cmd.kill()
await cmd.write_stdin(b"data\n")
```

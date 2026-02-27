---
sidebar_position: 6
title: Streaming
slug: /sdk/streaming
---

# SDK Streaming

Command output is streamed using `CommandOutputStream` (sync) and `AsyncCommandOutputStream` (async). These wrappers provide auto-reconnection and support both text and binary modes.

## Basic Usage

### Sync

```python
cmd = sandbox.commands.run(cmd="echo", args=["hello"])

with cmd.stdout() as stream:
    for chunk in stream:
        print(chunk, end="")
```

### Async

```python
cmd = await sandbox.commands.run(cmd="echo", args=["hello"])

async with cmd.stdout() as stream:
    async for chunk in stream:
        print(chunk, end="")
```

## Text vs Binary Mode

By default, commands run in **text mode** (`text=True`), where output is incrementally UTF-8 decoded and yielded as `str` chunks.

### Binary Mode

```python
cmd = sandbox.commands.run(cmd="cat", args=["/bin/ls"], text=False)

with cmd.stdout() as stream:
    for chunk in stream:
        # chunk is bytes
        process_bytes(chunk)
```

In binary mode, raw `bytes` chunks are yielded without decoding.

## Auto-Reconnect

Streams automatically reconnect on network errors using exponential backoff:

| Setting | Value |
|---------|-------|
| Initial backoff | 0.1 seconds |
| Backoff factor | 2x |
| Max backoff | 5 seconds |
| Max reconnect attempts | 5 |

Reconnection uses the `?offset=N` query parameter to resume from the last received byte, so no data is lost.

## Timeouts

### Read Timeout

The `timeout` parameter on `stdout()` / `stderr()` controls how long to wait for new data:

```python
from isola import StreamTimeoutError

try:
    with cmd.stdout(timeout=30.0) as stream:
        for chunk in stream:
            print(chunk, end="")
except StreamTimeoutError:
    print("No data received for 30 seconds")
```

If no data arrives within the timeout period, `StreamTimeoutError` is raised.

### Connection Timeouts

| Timeout | Value |
|---------|-------|
| Connect | 5 seconds |
| Write | 5 seconds |
| Pool | 5 seconds |

## Resuming Streams

You can manually resume a stream from a specific byte offset:

```python
# Start from byte 0
with cmd.stdout(offset=0) as stream:
    for chunk in stream:
        process(chunk)

# Resume from byte 1024
with cmd.stdout(offset=1024) as stream:
    for chunk in stream:
        process(chunk)
```

## Stream Lifecycle

1. **Open** — A context manager (`with` / `async with`) opens the HTTP connection
2. **Iterate** — Chunks are yielded as they arrive from the server
3. **Complete** — The stream ends when the command exits
4. **Close** — The context manager closes the HTTP connection

The stream connection remains open until:
- The command finishes (normal completion)
- A read timeout occurs (`StreamTimeoutError`)
- Too many reconnect failures (`APIConnectionError`)
- The context manager is exited

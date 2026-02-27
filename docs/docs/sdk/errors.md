---
sidebar_position: 7
title: Error Handling
slug: /sdk/errors
---

# SDK Error Handling

The SDK raises typed exceptions for different error conditions.

## Error Hierarchy

```
IsolaError (base)
├── BadRequestError        (HTTP 400)
├── NotFoundError          (HTTP 404)
├── ConflictError          (HTTP 409)
├── ValidationError        (HTTP 422)
├── InternalError          (HTTP 500)
├── BadGatewayError        (HTTP 502)
├── APIConnectionError     (transport failure)
└── StreamTimeoutError     (streaming timeout)
```

## IsolaError

All errors inherit from `IsolaError`, which has two attributes:

| Attribute | Type | Description |
|-----------|------|-------------|
| `status` | int | HTTP status code (0 for transport errors) |
| `detail` | str | Human-readable error message |

```python
from isola import IsolaError

try:
    sandbox = client.sandboxes.get("nonexistent")
except IsolaError as e:
    print(f"Status: {e.status}, Detail: {e.detail}")
```

## HTTP Errors

| Exception | Status | When |
|-----------|--------|------|
| `BadRequestError` | 400 | Invalid request body or parameters |
| `NotFoundError` | 404 | Sandbox or command not found |
| `ConflictError` | 409 | Sandbox not in a valid state for the operation |
| `ValidationError` | 422 | Request is well-formed but semantically invalid |
| `InternalError` | 500 | Unexpected server error |
| `BadGatewayError` | 502 | Sidecar communication failure |

```python
from isola import NotFoundError, ConflictError

try:
    sandbox = client.sandboxes.get("missing-id")
except NotFoundError:
    print("Sandbox does not exist")

try:
    cmd = sandbox.commands.run(cmd="echo", args=["hello"])
except ConflictError:
    print("Sandbox is not ready for commands")
```

## Transport Errors

### APIConnectionError

Raised when the SDK cannot reach the Isola API (network failure, DNS resolution failure, connection refused, etc.):

```python
from isola import APIConnectionError

try:
    client = Isola(base_url="http://unreachable:8080")
    client.sandboxes.list()
except APIConnectionError as e:
    print(f"Cannot reach API: {e.detail}")
```

`APIConnectionError` has `status=0`.

### StreamTimeoutError

Raised when no data is received on a stream within the specified timeout:

```python
from isola import StreamTimeoutError

try:
    with cmd.stdout(timeout=10.0) as stream:
        for chunk in stream:
            print(chunk, end="")
except StreamTimeoutError:
    print("Stream timed out - no data for 10 seconds")
```

`StreamTimeoutError` also inherits from Python's built-in `TimeoutError`, so you can catch either.

## Catching All Errors

```python
from isola import IsolaError

try:
    sandbox = client.sandboxes.create(image="python:3.12")
    cmd = sandbox.commands.run(cmd="echo", args=["hello"])
    with cmd.stdout() as stream:
        for chunk in stream:
            print(chunk, end="")
except IsolaError as e:
    print(f"Isola error: {e}")
```

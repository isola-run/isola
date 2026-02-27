---
sidebar_position: 2
title: Quickstart
slug: /sdk/quickstart
---

# Python SDK Quickstart

## Install

```bash
pip install isola
```

## Create a Client

```python
from isola import Isola

client = Isola(base_url="http://localhost:8080")
```

Or use it as a context manager:

```python
with Isola(base_url="http://localhost:8080") as client:
    # client is automatically closed at the end
    ...
```

## Create a Sandbox

```python
sandbox = client.sandboxes.create(
    image="python:3.12",
    cpu="500m",
    memory="512Mi",
    active_deadline_seconds=3600,
)

print(f"Sandbox {sandbox.id} is {sandbox.status}")
```

## Run a Command

```python
cmd = sandbox.commands.run(
    cmd="python",
    args=["-c", "print('Hello from Isola!')"],
)
```

## Read Output

```python
with cmd.stdout() as stream:
    for chunk in stream:
        print(chunk, end="")

# Check exit code
exit_code = cmd.exit_code()
print(f"Exit code: {exit_code}")
```

## Transfer Files

```python
# Upload
sandbox.filesystem.write("/workspace/script.py", b"print('hello')")

# Download
content = sandbox.filesystem.read("/workspace/script.py")
print(content.decode())
```

## Clean Up

```python
sandbox.delete()
client.close()
```

## Full Example

```python
from isola import Isola

with Isola(base_url="http://localhost:8080") as client:
    # Create a sandbox with resource limits
    sandbox = client.sandboxes.create(
        image="python:3.12",
        cpu="500m",
        memory="256Mi",
    )

    # Upload a script
    sandbox.filesystem.write(
        "/workspace/hello.py",
        b"import sys\nprint(f'Python {sys.version}')\n",
    )

    # Run the script
    cmd = sandbox.commands.run(
        cmd="python",
        args=["/workspace/hello.py"],
    )

    # Stream stdout
    with cmd.stdout() as stream:
        for chunk in stream:
            print(chunk, end="")

    # Verify success
    assert cmd.exit_code() == 0

    # Clean up
    sandbox.delete()
```

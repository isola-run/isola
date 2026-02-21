# isola Python SDK

Python SDK for the Isola sandbox API.

## Install

```bash
pip install isola
```

## Quick start

Set the `ISOLA_BASE_URL` environment variable:

```bash
export ISOLA_BASE_URL=http://localhost:8080
```

```python
from isola import Isola

client = Isola()
sandbox = client.sandboxes.create(image="alpine:3.21")

# Run a command and get output
cmd = sandbox.commands.run("echo", "hello world")
print(cmd.stdout.read())   # "hello world\n"
print(cmd.exit_code())     # 0

sandbox.delete()
```

## Streaming output

```python
cmd = sandbox.commands.spawn("sh", "-c", "for i in 1 2 3; do echo line$i; sleep 1; done")
for chunk in cmd.stdout:
    print(chunk, end="")
cmd.wait()
```

## Stdin

```python
# Pipe input to a command
cmd = sandbox.commands.run("cat", input="hello from stdin\n")
print(cmd.stdout.read())  # "hello from stdin\n"

# Manual stdin control
cmd = sandbox.commands.spawn("cat")
cmd.write_stdin("hello\n")
cmd.close_stdin()
cmd.wait()
print(cmd.stdout.read())  # "hello\n"
```

## File I/O

```python
sandbox.filesystem.write("/tmp/hello.txt", b"hello world")
data = sandbox.filesystem.read("/tmp/hello.txt")  # bytes
```

## Async client

```python
from isola import AsyncIsola

async with AsyncIsola() as client:
    sandbox = await client.sandboxes.create(image="alpine:3.21")
    cmd = await sandbox.commands.run("echo", "hello")
    print(await cmd.stdout.read())
    await sandbox.delete()
```

## Configuration

The base URL can be set via environment variable or constructor argument:

```python
# From environment variable (recommended)
client = Isola()  # reads ISOLA_BASE_URL

# Explicit
client = Isola(base_url="http://localhost:8080")
```

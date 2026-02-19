# isola Python SDK

Python SDK for the Isola sandbox API.

## Install

```bash
pip install isola
```

## Sync client

```python
from isola import Isola

with Isola(base_url="http://localhost:8080") as client:
    sandbox = client.sandboxes.create(image="python:3.12")
    cmd = sandbox.commands.run(cmd="python", args=["-c", "print('hello')"])

    with cmd.stdout() as stream:
        for chunk in stream:
            print(chunk.decode(), end="")

    sandbox.delete()
```

## Async client

```python
from isola import AsyncIsola

async with AsyncIsola(base_url="http://localhost:8080") as client:
    sandbox = await client.sandboxes.create(image="python:3.12")
    cmd = await sandbox.commands.run(cmd="ls", args=["-la"])

    async with cmd.stdout() as stream:
        async for chunk in stream:
            print(chunk.decode(), end="")

    await sandbox.delete()
```

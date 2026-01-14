# Isola Python SDK

Remote sandbox execution made simple.

## Installation

```bash
pip install isola
```

## Quick Start

```python
import isola

sandbox = isola.Sandbox("my-sandbox").image("python:3.11-slim")

@sandbox.function()
def process(data):
    return sum(data)

result = process.remote([1, 2, 3, 4, 5])
print(result)  # 15
```

## Documentation

See the [examples/](examples/) directory for more usage patterns.

# Tutorials

Step-by-step guides for common Isola use cases. Each tutorial includes working code examples you can run immediately.

---

## Tutorials

| Tutorial | Description | Difficulty |
|----------|-------------|------------|
| [AI Code Execution](./ai-code-execution.md) | Run LLM-generated code safely | Beginner |
| [Data Processing Pipeline](./data-pipeline.md) | Process data with isolated workers | Intermediate |
| [Multi-tenant SaaS](./multi-tenant.md) | Isolate customer workloads | Advanced |
| [CI/CD Integration](./ci-cd-integration.md) | Use sandboxes in pipelines | Intermediate |

---

## Quick Reference

### Sandbox Lifecycle

```python
# 1. Create
sandbox = client.create_sandbox(name="tutorial", auto_start=True)

# 2. Wait for ready
sandbox = client.wait_for_state(sandbox['id'], "running")

# 3. Use
result = client.execute_command(sandbox['id'], "echo hello")

# 4. Cleanup
client.terminate_sandbox(sandbox['id'])
```

### Common Operations

```python
# Execute command
result = client.execute_command(id, "python script.py")
print(result['stdout'])

# Upload file
client.upload_file(id, "/workspace/data.json", json.dumps(data).encode())

# Run uploaded file
result = client.execute_command(id, "python /workspace/script.py")
```

---

## Prerequisites

All tutorials assume:

1. **Isola is running** (local or cluster)
2. **API key configured**
3. **Python 3.8+** (for SDK examples)

### Setup

```bash
# Start local development environment
cd isola
./scripts/setup.sh
tilt up

# Set environment variables
export ISOLA_API="http://localhost:8080"
export ISOLA_API_KEY="iso_sk_demo"
```

### Python SDK

```bash
# Install client (from tests directory)
cd tests
pip install -e client/
```

Or use the client directly:

```python
# Copy isola_client.py to your project
from isola_client import IsolaClient

client = IsolaClient(
    base_url=os.environ["ISOLA_API"],
    api_key=os.environ["ISOLA_API_KEY"]
)
```

### test
uv run -m pytest

### lint / format
uv run ruff check --fix .

### type check
uv run mypy .

### install
uv sync # installs main project.dependencies deps in .venv
uv sync --dev # installs development dependency-groups.dev deps in .venv

### dependencies
uv add <dependency> # adds a new project dependency (e.g. uv add websockets)
uv add --dev <dependency> # adds a new development dependency (e.g. uv add --dev pytest)

### Quick curl commands
curl -X POST http://localhost:30080/sandboxes \
  -H "Content-Type: application/json" \
  -H "X-API-Key: iso_sk_demo" \
  -d '{
    "name": "test-sandbox"
  }'

# Bash commands
curl -X POST http://localhost:30080/sandboxes/YOUR_SANDBOX_ID/execute \
  -H "Content-Type: application/json" \
  -H "X-API-Key: iso_sk_demo" \
  -d "{\"command\": \"echo 'hello from sandbox'\"}"

# List all sandboxes
curl -X GET "http://localhost:30080/sandboxes" \
  -H "X-API-Key: iso_sk_demo"
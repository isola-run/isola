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
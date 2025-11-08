### test
uv run -m pytest

### lint / format
uv run ruff check --fix .

### type check
uv run mypy .

### install
uv sync # installs main project.dependencies deps in .venv
uv sync --dev # installs development dependency-groups.dev deps in .venv
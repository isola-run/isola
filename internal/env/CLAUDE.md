# internal/env/

Environment variable parsing utilities.

- `GetOrDefault(key, defaultValue)` — Returns env var value or default
- `GetOrDefaultInt(key, defaultValue)` — Returns env var as int or default

Used by cmd entry points for flag defaults (e.g., `ISOLA_HTTP_PORT`, `ISOLA_LOG_LEVEL`).

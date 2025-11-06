dev-isola/
├── services/                             
│   ├── isola-controller/
│   │   ├── app/
│   │   │   ├── api/
│   │   │   │   ├── v1/
│   │   │   │   │   ├── endpoints/
│   │   │   │   │   │   ├── sandboxes.py
│   │   │   │   │   │   └── health.py
│   │   │   │   │   └── router.py
│   │   │   │   └── __init__.py
│   │   │   ├── src/
│   │   │   │   ├── sandbox_service.py    # Business logic from SandboxStore
│   │   │   │   ├── agent_manager.py     # From isola/control/agent_manager.py
│   │   │   └── main.py                  # FastAPI app initialization
│   │   ├── tests/
│   │   ├── pyproject.toml
│   │   ├── Dockerfile
│   │   └── README.md
│   │
│   └── isola-agent/
│       ├── app/
│       │   ├── api/
│       │   │   └── v1/
│       │   │       ├── endpoints/
│       │   │       │   ├── executor.py  # Execute code
│       │   │       │   ├── health.py
│       │   │       │   └── metrics.py
│       │   │       └── router.py
│       │   ├── src/
│       │   │   ├── sandbox_runtime.py   # Container/isolation management
│       │   │   └── resource_monitor.py  # Resource usage tracking
│       │   ├── websocket/
│       │   │   └── control_client.py
│       │   └── main.py
│       ├── tests/
│       ├── pyproject.toml
│       ├── Dockerfile
│       └── README.md
│
├── common/                               # Shared library
│   ├── isola_common/
│   │   ├── models/                      
│   │   │   ├── sandbox.py               # Pydantic models used by all services
│   │   │   ├── runner.py
│   │   │   ├── execution.py             # CodeExecutionRequest/Response
│   │   │   └── __init__.py
│   │   ├── auth/
│   │   │   └── api_key.py              # Shared auth logic
│   │   ├── utils/
│   │   │   ├── logging.py
│   │   │   └── validators.py
│   │   └── __init__.py
│   ├── tests/
│   ├── pyproject.toml
│   └── README.md
│

├── docs/                                 # Documentation
│   ├── api/
│   │   └── openapi.yaml                 # From demo/isola-api-spec.yaml
│
├── scripts/                              # Development scripts
│   └── script.sh
│
├── .github/                              # CI/CD
├── pyproject.toml                        # Root project configuration
├── uv.lock
├── README.md
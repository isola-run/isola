
from __future__ import annotations

import asyncio
from contextlib import asynccontextmanager
import logging
import uuid
from datetime import datetime
from typing import Any, Dict, List, Optional

from fastapi.responses import PlainTextResponse
from fastapi import BackgroundTasks, FastAPI, HTTPException, Query, Security
from fastapi.middleware.cors import CORSMiddleware
from fastapi.security import APIKeyHeader

# Import Pydantic models and enums from dedicated modules
from isola.control.agent_manager import AgentManager
from isola.models.common import Error
from isola.models.sandbox import (
    SandboxState,
    Sandbox,
    CreateSandbox,
    SandboxList,
)
from isola.models.agent_ws import CreateSandboxRequest, CreateSandboxResponse

logger = logging.getLogger(__name__)

# todo benl: move this logic to somewhere more appropriate (if we keep)
@asynccontextmanager
async def lifespan(app: FastAPI):
    agent_manager = AgentManager()
    # app.state.agent_manager = agent_manager
    await agent_manager.start()     # spawns uvicorn WS server in the background
    try:
        yield
    finally:
        await agent_manager.shutdown()


app = FastAPI(
    title="Isola Sandbox Infrastructure API",
    version="1.0.0",
    description=(
        "Isola provides on-premise sandbox infrastructure for development and testing environments.\n"
        "This API allows you to manage sandboxes, snapshots, and volumes programmatically."
    ),
    contact={
        "name": "Isola Support",
        "email": "support@isola.run",
        "url": "https://isola.run",
    },
    license_info={
        "name": "MIT",
        "url": "https://opensource.org/licenses/MIT",
    },
    servers=[
    {"url": "http://localhost:3000", "description": "local environment"},
    {"url": "https://api.isola.run", "description": "Production environment"},
    ],
    lifespan=lifespan
)


# CORS for convenience in demos
# app.add_middleware(
#     CORSMiddleware,
#     allow_origins=["*"],
#     allow_credentials=True,
#     allow_methods=["*"],
#     allow_headers=["*"],
# )


# Security scheme for API Key in header
#TODO: auto_error=True
api_key_header = APIKeyHeader(name="X-API-Key", scheme_name="apiKey", auto_error=False)


def tenant_from_api_key(api_key: Optional[str]) -> str:
    # For demo: require presence of header except where spec says security: []
    if api_key is None or api_key == "":
        raise HTTPException(status_code=401, detail="Unauthorized: missing API key")
    if api_key == "iso_sk_a1b2c3d4e5f67890a1b2c3d4e5f67890":
        return "2280e575-f37d-4329-b033-9de263ce7625"
    if api_key == "iso_sk_demo":
        return "e766a1e8-4b0e-4bb7-9612-80b9c1c8cd87"


# In-memory storage for sandboxes: tenant_id -> {sandbox_id -> Sandbox}
sandboxes: Dict[str, Dict[str, Sandbox]] = {}

# Import the shared agent_manager instance
# Try to use the instance from main.py if available, otherwise create our own
_agent_manager = None 
_agent_manager_started = False

def get_agent_manager():
    """Get the shared AgentManager instance"""
    global _agent_manager, _agent_manager_started
    
    # If already set (e.g., by main.py), use it
    if _agent_manager is not None:
        return _agent_manager
    
    # Otherwise, create a new one (for standalone uvicorn usage)
    from isola.control.agent_manager import AgentManager
    _agent_manager = AgentManager()
    
    if not _agent_manager_started:
        _agent_manager_started = True
        asyncio.create_task(_agent_manager.start())
        logger.info("Auto-created AgentManager on ws://localhost:8765")
    
    return _agent_manager

# Sandboxes
@app.get(
    "/sandboxes",
    response_model=SandboxList,
    tags=["sandboxes"],
    summary="List all sandboxes",
    description="Returns a list of all sandboxes",
    operation_id="listSandboxes",
    responses={
        200: {"description": "List of sandboxes"},
        401: {"description": "Unauthorized - Invalid or missing API key", "model": Error},
    },
)
async def list_sandboxes(
    state: Optional[SandboxState] = Query(default=None),
    limit: int = Query(default=20, ge=1, le=100),
    offset: int = Query(default=0, ge=0),
    api_key: Optional[str] = Security(api_key_header),
):
    tenant_id = tenant_from_api_key(api_key)
    
    tenant_sandboxes = sandboxes.get(tenant_id, {})
    
    # Filter by state if provided
    items = list(tenant_sandboxes.values())
    if state:
        items = [s for s in items if s.state == state]
    
    # Apply pagination
    total = len(items)
    items = items[offset:offset + limit]
    
    return SandboxList(
        items=items,
        total=total,
        limit=limit,
        offset=offset
    )


@app.post(
    "/sandboxes",
    response_model=Sandbox,
    status_code=201,
    tags=["sandboxes"],
    summary="Create a new sandbox",
    description="Creates a new sandbox with the specified configuration",
    operation_id="createSandbox",
    responses={
        201: {"description": "Sandbox created successfully"},
        400: {"description": "Bad request - Invalid input", "model": Error},
        401: {"description": "Unauthorized - Invalid or missing API key", "model": Error},
        409: {"description": "Conflict - Resource already exists or state conflict", "model": Error},
    },
)
async def create_sandbox(
    req: CreateSandbox,
    api_key: Optional[str] = Security(api_key_header),
):
    tenant_id = tenant_from_api_key(api_key)
    
    # Generate sandbox ID
    sandbox_id = str(uuid.uuid4())
    now = datetime.utcnow()
    
    # Create sandbox object
    sandbox_data = {
        "id": sandbox_id,
        "name": req.name,
        "state": SandboxState.creating.value,
        "desiredState": (SandboxState.started if req.autoStart else SandboxState.stopped).value,
        "class": req.class_.value if req.class_ else "small",
        "region": req.region if req.region else "default",
        "image": req.image or "python:3.11",
        "cpu": req.cpu or 1,
        "memory": req.memory or 1,
        "disk": req.disk or 10,
        "gpu": req.gpu or 0,
        "env": req.env or {},
        "labels": req.labels or {},
        "volumes": req.volumes or [],
        "ports": [],
        "runnerId": None,
        "errorReason": None,
        "ipAddress": None,
        "createdAt": now.isoformat(),
        "updatedAt": now.isoformat(),
        "lastActivityAt": None
    }
    sandbox = Sandbox.model_validate(sandbox_data)
    
    # Store sandbox in memory
    if tenant_id not in sandboxes:
        sandboxes[tenant_id] = {}
    sandboxes[tenant_id][sandbox_id] = sandbox
    
    # Create request for isolad agent
    agent_request = CreateSandboxRequest(
        sandbox_id=sandbox_id,
        name=req.name,
        image=req.image or "python:3.11",
        cpu=float(req.cpu or 1),
        memory=float(req.memory or 1),
        disk=float(req.disk or 10),
        env=req.env or {},
        labels=req.labels or {}
    )
    
    # Send request to agent via AgentManager
    agent_manager = get_agent_manager()
    # Run async communication in background
    asyncio.create_task(_handle_sandbox_creation(
        tenant_id, sandbox_id, agent_request, agent_manager
    ))
    
    return sandbox


async def _handle_sandbox_creation(
    tenant_id: str,
    sandbox_id: str, 
    request: CreateSandboxRequest,
    agent_manager
):
    """Handle sandbox creation with agent asynchronously"""
    try:
        logger.info(f"Sending sandbox creation request to agent: {request}")
        response = await agent_manager.send_create_sandbox_request(request)
        logger.info(f"Sandbox creation response: {response}")
        
        if sandbox_id in sandboxes.get(tenant_id, {}):
            sandbox = sandboxes[tenant_id][sandbox_id]
            
            if response and response.success:
                # Update sandbox state based on desired state
                if sandbox.desiredState == SandboxState.started:
                    sandbox.state = SandboxState.running   
                else:
                    sandbox.state = SandboxState.stopped
                sandbox.ipAddress = response.ip_address
                # Note: agent_id is not in CreateSandboxResponse currently
            else:
                sandbox.state = SandboxState.error
                sandbox.errorReason = response.error_reason if response else "No agent available"
            
            sandbox.updatedAt = datetime.utcnow()
            logger.info(f"Sandbox {sandbox_id} creation completed with state: {sandbox.state}")
    except Exception as e:
        logger.error(f"Error creating sandbox {sandbox_id}: {e}")
        if sandbox_id in sandboxes.get(tenant_id, {}):
            sandbox = sandboxes[tenant_id][sandbox_id]
            sandbox.state = SandboxState.error
            sandbox.errorReason = str(e)
            sandbox.updatedAt = datetime.utcnow()

@app.get(
    "/sandboxes/{sandbox_id}",
    response_model=Sandbox,
    tags=["sandboxes"],
    summary="Get sandbox details",
    description="Returns detailed information about a specific sandbox",
    operation_id="getSandbox",
    responses={
        200: {"description": "Sandbox details"},
        401: {"description": "Unauthorized - Invalid or missing API key", "model": Error},
        404: {"description": "Resource not found", "model": Error},
    },
)
async def get_sandbox(sandbox_id: str, api_key: Optional[str] = Security(api_key_header)):
    tenant_id = tenant_from_api_key(api_key)
    
    tenant_sandboxes = sandboxes.get(tenant_id, {})
    if sandbox_id not in tenant_sandboxes:
        raise HTTPException(status_code=404, detail="Sandbox not found")
    
    return tenant_sandboxes[sandbox_id]


@app.delete(
    "/sandboxes/{sandbox_id}",
    status_code=204,
    tags=["sandboxes"],
    summary="Delete a sandbox",
    description="Permanently deletes a sandbox and all associated resources",
    operation_id="deleteSandbox",
    responses={
        204: {"description": "Sandbox deleted successfully"},
        401: {"description": "Unauthorized - Invalid or missing API key", "model": Error},
        404: {"description": "Resource not found", "model": Error},
        409: {"description": "Conflict - Resource already exists or state conflict", "model": Error},
    },
)
async def delete_sandbox(
    sandbox_id: str,
    force: bool = Query(default=False),
    api_key: Optional[str] = Security(api_key_header),
):
    tenant_from_api_key(api_key)
    return None

@app.post(
    "/sandboxes/{sandbox_id}/stop",
    response_model=Sandbox,
    status_code=202,
    tags=["sandboxes"],
    summary="Stop a sandbox",
    description="Stops a running sandbox",
    operation_id="stopSandbox",
    responses={
        202: {"description": "Sandbox stop initiated"},
        400: {"description": "Bad request - Invalid input", "model": Error},
        401: {"description": "Unauthorized - Invalid or missing API key", "model": Error},
        404: {"description": "Resource not found", "model": Error},
        409: {"description": "Conflict - Resource already exists or state conflict", "model": Error},
    },
)
async def stop_sandbox(sandbox_id: str, api_key: Optional[str] = Security(api_key_header)):
    tenant_from_api_key(api_key)


@app.post(
    "/sandboxes/{sandbox_id}/restart",
    response_model=Sandbox,
    status_code=202,
    tags=["sandboxes"],
    summary="Restart a sandbox",
    description="Restarts a sandbox (stop and start)",
    operation_id="restartSandbox",
    responses={
        202: {"description": "Sandbox restart initiated"},
        401: {"description": "Unauthorized - Invalid or missing API key", "model": Error},
        404: {"description": "Resource not found", "model": Error},
    },
)
async def restart_sandbox(sandbox_id: str, api_key: Optional[str] = Security(api_key_header)):
    tenant_from_api_key(api_key)



# Convenience for external import
def get_app() -> FastAPI:
    return app

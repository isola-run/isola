
from __future__ import annotations

import asyncio
from contextlib import asynccontextmanager
import logging
import uuid
from datetime import datetime
from typing import Dict, Optional
import os
import sys
from fastapi import FastAPI, HTTPException, Query, Security, Response
from fastapi.security import APIKeyHeader

from common.models.common import Error
from common.models.control_protocol import CreateSandboxRequest
from common.models.sandbox import (
    ExecuteCommandRequest,
    ExecuteCommandResponse,
    SandboxState,
    Sandbox,
    CreateSandbox,
    SandboxList,
)
from services.isola_controller.agent_manager import AgentManager
from services.isola_controller.kubernetes_control.sandboxes import KubernetesManager

logger = logging.getLogger()
if not logger.handlers:
    log_level = os.getenv("LOG_LEVEL", "info").upper()
    logger.setLevel(getattr(logging, log_level, logging.INFO))

    handler = logging.StreamHandler(sys.stdout)
    handler.setLevel(getattr(logging, log_level, logging.INFO))

    formatter = logging.Formatter(
        fmt="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
        datefmt="%Y-%m-%d %H:%M:%S",
    )
    handler.setFormatter(formatter)

    logger.addHandler(handler)

logger = logging.getLogger(__name__)


agent_manager = AgentManager()
SANDBOX_BACKENDS = {"agent", "kubernetes"}
SANDBOX_BACKEND = os.getenv("SANDBOX_BACKEND", "agent").lower()
SANDBOX_BACKEND = "kubernetes"
if SANDBOX_BACKEND not in SANDBOX_BACKENDS:
    logger.warning(
        "Unsupported SANDBOX_BACKEND=%s detected, defaulting to 'agent'",
        SANDBOX_BACKEND,
    )
    SANDBOX_BACKEND = "agent"

KUBERNETES_NAMESPACE = os.getenv("KUBERNETES_NAMESPACE", "isola-sandboxes")
MINIKUBE_API_SERVER = "https://192.168.49.2:8443"

kubernetes_manager = KubernetesManager(
    namespace=KUBERNETES_NAMESPACE,
    api_server_url=MINIKUBE_API_SERVER,
)

# todo benl: move this logic to somewhere more appropriate (if we keep)
@asynccontextmanager
async def lifespan(app: FastAPI):
    await agent_manager.start()     # spawns uvicorn WS server in the background
    if SANDBOX_BACKEND == "kubernetes":
        await kubernetes_manager.initialize()
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
    {"url": "http://isola-controller:30080", "description": "local environment"},
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
    return "e766a1e8-4b0e-4bb7-9612-80b9c1c8cd87"



# Health Check
@app.get(
    "/health",
    tags=["system"],
    summary="Health check",
    description="Check if the API is running and healthy",
    responses={
        200: {"description": "Service is healthy"},
        503: {"description": "Service is unhealthy"}
    }
)
async def health_check():
    """Health check endpoint"""
    health_status = {
        "status": "healthy",
        "timestamp": datetime.utcnow().isoformat(),
        "components": {
            "api": "healthy",
            "agent_manager": "healthy" if agent_manager else "unhealthy",
            "websocket_server": "healthy"  # Assuming it's healthy if the app is running
        },
        "version": "1.0.0"
    }
    
    # Check if we have any active agents
    try:
        agent_count = len(agent_manager._active_agents) if hasattr(agent_manager, '_active_agents') else 0
        health_status["agent_count"] = agent_count
    except:
        health_status["agent_count"] = 0
    
    return health_status

# Readiness Check
@app.get(
    "/ready",
    tags=["system"],
    summary="Readiness check",
    description="Check if the API is ready to serve traffic",
    responses={
        200: {"description": "Service is ready"},
        503: {"description": "Service is not ready"}
    }
)
async def readiness_check():
    """Readiness check endpoint - verifies all dependencies are ready"""
    # TODO: __OMER__ Check if agent manager is running, etc
    return {"status": "ready"}

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
    
    # Get sandboxes directly from Kubernetes
    if SANDBOX_BACKEND == "kubernetes":
        pods = await kubernetes_manager.list_pods()
        items = []
        for pod_data in pods:
            sandbox = Sandbox.model_validate(pod_data)
            if state is None or sandbox.state == state:
                items.append(sandbox)
    else:
        # For agent backend, return empty list for now
        items = []
    
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
    desired_state = SandboxState.running if req.autoStart else SandboxState.stopped
    
    # Create sandbox object for response
    sandbox_data = {
        "id": sandbox_id,
        "name": req.name,
        "state": SandboxState.pending,
        "desiredState": desired_state,
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
        "createdAt": now,
        "updatedAt": now,
        "lastActivityAt": None
    }
    sandbox = Sandbox.model_validate(sandbox_data)
    logger.info(f"Creating sandbox: {sandbox}")
    
    # Create request for backend
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
    
    # Send request to backend
    # Run async communication in background
    asyncio.create_task(_handle_sandbox_creation(
        tenant_id, sandbox_id, agent_request, agent_manager, req.autoStart
    ))
    
    logger.info(f"Sandbox request created: {sandbox}")
    return sandbox


async def _handle_sandbox_creation(
    tenant_id: str,
    sandbox_id: str, 
    request: CreateSandboxRequest,
    agent_manager,
    auto_start: bool,
):
    """Handle sandbox creation asynchronously using the configured backend"""
    if SANDBOX_BACKEND == "kubernetes":
        await _handle_kubernetes_sandbox_creation(
            tenant_id, sandbox_id, request, auto_start
        )
        return
    
    await _handle_agent_sandbox_creation(
        tenant_id, sandbox_id, request, agent_manager
    )


async def _handle_agent_sandbox_creation(
    tenant_id: str,
    sandbox_id: str,
    request: CreateSandboxRequest,
    agent_manager: AgentManager,
):
    """Delegate sandbox creation to an Isola agent over WebSocket"""
    try:
        logger.info("Sending sandbox creation request to agent: %s", request)
        response = await agent_manager.send_create_sandbox_request(request)
        logger.info("Sandbox creation response: %s", response)
        
        if response and response.success:
            logger.info(f"Sandbox {sandbox_id} created successfully")
        else:
            error_reason = response.error_reason if response else "No agent available"
            logger.error(f"Sandbox {sandbox_id} creation failed: {error_reason}")
    except Exception as e:
        logger.error(f"Error creating sandbox {sandbox_id}: {e}")


async def _handle_kubernetes_sandbox_creation(
    tenant_id: str,
    sandbox_id: str,
    request: CreateSandboxRequest,
    auto_start: bool,
):
    """Provision the sandbox directly via the Kubernetes manager"""
    try:
        success, ip_address, error_reason = await kubernetes_manager.create_pod(
            sandbox_id=sandbox_id,
            name=request.name,
            image=request.image,
            cpu=request.cpu,
            memory=request.memory,
            disk=request.disk,
            env=request.env,
            labels=request.labels,
            auto_start=auto_start,
        )

        logger.info(
            "Sandbox %s Kubernetes provisioning result success=%s ip=%s reason=%s",
            sandbox_id,
            success,
            ip_address,
            error_reason,
        )
    except Exception as exc:
        logger.error("Error creating sandbox %s via Kubernetes: %s", sandbox_id, exc)

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
    # TODO:__ISO2__ validate tenant_id belongs to the sandbox
    tenant_id = tenant_from_api_key(api_key)
    
    # Get sandbox directly from Kubernetes
    if SANDBOX_BACKEND == "kubernetes":
        state, ip_address, error_reason = await kubernetes_manager.get_pod_status(sandbox_id)
        if state is None:
            raise HTTPException(status_code=404, detail="Sandbox not found")
        
        # Construct sandbox object from pod status
        # Note: This is simplified - in production you'd want more metadata
        sandbox = Sandbox.model_validate({
            "id": sandbox_id,
            "name": f"sandbox-{sandbox_id[:8]}",
            "state": state,
            "desiredState": state,
            "class": "small",
            "region": "default",
            "image": "unknown",
            "cpu": 1,
            "memory": 1,
            "disk": 10,
            "gpu": 0,
            "env": {},
            "labels": {},
            "volumes": [],
            "ports": [],
            "runnerId": None,
            "errorReason": error_reason,
            "ipAddress": ip_address,
            "createdAt": datetime.utcnow(),
            "updatedAt": datetime.utcnow(),
            "lastActivityAt": None
        })
        return sandbox
    else:
        raise HTTPException(status_code=404, detail="Sandbox not found")


@app.delete(
    "/sandboxes/{sandbox_id}",
    status_code=204,
    tags=["sandboxes"],
    summary="Terminate a sandbox",
    description="Terminates a sandbox and all associated resources",
    operation_id="terminateSandbox",
    responses={
        204: {"description": "Sandbox terminated successfully"},
        401: {"description": "Unauthorized - Invalid or missing API key", "model": Error},
        404: {"description": "Resource not found", "model": Error},
        409: {"description": "Conflict - Resource already exists or state conflict", "model": Error},
    },
)
async def terminate_sandbox(
    sandbox_id: str,
    force: bool = Query(default=False),
    api_key: Optional[str] = Security(api_key_header),
):
    tenant_id = tenant_from_api_key(api_key)
    
    if SANDBOX_BACKEND == "kubernetes":
        success, error_reason = await kubernetes_manager.terminate_pod(
            sandbox_id, force=force
        )
        if not success:
            status_code = 404 if error_reason == "Pod not found" else 500
            raise HTTPException(
                status_code=status_code,
                detail=error_reason or "Failed to delete sandbox",
            )
    else:
        raise HTTPException(
            status_code=501,
            detail="Sandbox deletion is only implemented for the Kubernetes backend",
        )

    return Response(status_code=204)


# ============================================================================
# Command Execution Routes
# ============================================================================

@app.post(
    "/sandboxes/{sandbox_id}/execute",
    response_model=ExecuteCommandResponse,
    status_code=200,
    tags=["execution"],
    summary="Execute a command in a sandbox",
    description="Execute a command in the specified sandbox",
    operation_id="executeCommand",
    responses={
        200: {"description": "Command executed successfully"},
        401: {"description": "Unauthorized - Invalid or missing API key", "model": Error},
        404: {"description": "Sandbox not found", "model": Error},
        409: {"description": "Conflict - Sandbox not in running state", "model": Error},
        501: {"description": "Not implemented for non-Kubernetes backend", "model": Error},
    }
)
async def execute_command(
    sandbox_id: str,
    command_request: ExecuteCommandRequest,
    api_key: Optional[str] = Security(api_key_header)
):
    """Execute a command in a sandbox."""
    # TODO:__ISO2__ validate tenant_id belongs to the sandbox
    tenant_id = tenant_from_api_key(api_key)
    
    if SANDBOX_BACKEND != "kubernetes":
        raise HTTPException(
            status_code=501,
            detail="Command execution is only implemented for the Kubernetes backend"
        )
    
    # Check sandbox exists and is running
    state, _, _ = await kubernetes_manager.get_pod_status(sandbox_id)
    if state is None:
        raise HTTPException(status_code=404, detail="Sandbox not found")
    
    if state != SandboxState.running:
        raise HTTPException(
            status_code=409,
            detail=f"Sandbox must be in 'running' state, current state: {state}"
        )
    
    # Execute command in Kubernetes pod
    stdout, stderr, exit_code = await kubernetes_manager.execute_command(
        sandbox_id, 
        command_request.command
    )
    
    return ExecuteCommandResponse(
        stdout=stdout,
        stderr=stderr,
        exitCode=exit_code
    )


# Convenience for external import
def get_app() -> FastAPI:
    return app

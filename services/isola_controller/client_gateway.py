
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
MINIKUBE_CA_CERT = "/etc/ssl/ca.crt"
MINIKUBE_CLIENT_CERT = "/etc/ssl/client.crt"
MINIKUBE_CLIENT_KEY = "/etc/ssl/client.key"

kubernetes_manager = KubernetesManager(
    namespace=KUBERNETES_NAMESPACE,
    api_server_url=MINIKUBE_API_SERVER,
    ca_cert_path=MINIKUBE_CA_CERT,
    client_cert_path=MINIKUBE_CLIENT_CERT,
    client_key_path=MINIKUBE_CLIENT_KEY,
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


# In-memory storage for sandboxes: tenant_id -> {sandbox_id -> Sandbox}
sandboxes: Dict[str, Dict[str, Sandbox]] = {}

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
    await _sync_tenant_sandboxes_with_backend(tenant_id)
    
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
    desired_state = SandboxState.started if req.autoStart else SandboxState.stopped
    
    # Create sandbox object
    sandbox_data = {
        "id": sandbox_id,
        "name": req.name,
        "state": SandboxState.creating,
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
    # Run async communication in background
    asyncio.create_task(_handle_sandbox_creation(
        tenant_id, sandbox_id, agent_request, agent_manager, req.autoStart
    ))
    
    logger.info(f"Sandbox requestcreated: {sandbox}")
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
        
        if sandbox_id in sandboxes.get(tenant_id, {}):
            sandbox = sandboxes[tenant_id][sandbox_id]
            
            if response and response.success:
                # Update sandbox state based on desired state
                if sandbox.desiredState == SandboxState.started:
                    sandbox.state = SandboxState.started   
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


async def _handle_kubernetes_sandbox_creation(
    tenant_id: str,
    sandbox_id: str,
    request: CreateSandboxRequest,
    auto_start: bool,
):
    """Provision the sandbox directly via the Kubernetes manager"""
    tenant_sandboxes = sandboxes.get(tenant_id, {})
    sandbox = tenant_sandboxes.get(sandbox_id)
    if sandbox is None:
        logger.warning("Sandbox %s vanished before provisioning", sandbox_id)
        return

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

        if success:
            target_state = SandboxState.started if auto_start else SandboxState.stopped
            sandbox.state = target_state
            sandbox.desiredState = target_state
            sandbox.ipAddress = ip_address
            sandbox.errorReason = None
        else:
            sandbox.state = SandboxState.error
            sandbox.errorReason = error_reason or "Failed to create sandbox pod"

        sandbox.updatedAt = datetime.utcnow()
        logger.info(
            "Sandbox %s Kubernetes provisioning result success=%s ip=%s reason=%s",
            sandbox_id,
            success,
            ip_address,
            error_reason,
        )
    except Exception as exc:
        logger.error("Error creating sandbox %s via Kubernetes: %s", sandbox_id, exc)
        sandbox.state = SandboxState.error
        sandbox.errorReason = str(exc)
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
    await _sync_tenant_sandboxes_with_backend(tenant_id, [sandbox_id])
    return _get_sandbox_or_404(tenant_id, sandbox_id)


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
    tenant_id = tenant_from_api_key(api_key)
    _ = _get_sandbox_or_404(tenant_id, sandbox_id)

    if SANDBOX_BACKEND == "kubernetes":
        success, error_reason = await kubernetes_manager.delete_pod(
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

    tenant_sandboxes = sandboxes.get(tenant_id, {})
    tenant_sandboxes.pop(sandbox_id, None)
    if not tenant_sandboxes:
        sandboxes.pop(tenant_id, None)

    return Response(status_code=204)

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
    tenant_id = tenant_from_api_key(api_key)
    sandbox = _get_sandbox_or_404(tenant_id, sandbox_id)
    sandbox.desiredState = SandboxState.stopped
    sandbox.state = SandboxState.stopping

    if SANDBOX_BACKEND != "kubernetes":
        raise HTTPException(
            status_code=501,
            detail="Sandbox stop is only implemented for the Kubernetes backend",
        )

    success, error_reason = await kubernetes_manager.stop_pod(sandbox_id)
    if not success:
        status_code = 404 if error_reason == "Pod not found" else 409
        raise HTTPException(
            status_code=status_code,
            detail=error_reason or "Failed to stop sandbox",
        )

    await _sync_tenant_sandboxes_with_backend(tenant_id, [sandbox_id])
    sandbox.updatedAt = datetime.utcnow()
    return sandbox


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
    tenant_id = tenant_from_api_key(api_key)
    sandbox = _get_sandbox_or_404(tenant_id, sandbox_id)
    sandbox.desiredState = SandboxState.started
    sandbox.state = SandboxState.starting

    if SANDBOX_BACKEND != "kubernetes":
        raise HTTPException(
            status_code=501,
            detail="Sandbox restart is only implemented for the Kubernetes backend",
        )

    success, ip_address, error_reason = await kubernetes_manager.restart_pod(sandbox_id)
    if not success:
        status_code = 404 if error_reason == "Pod not found" else 409
        raise HTTPException(
            status_code=status_code,
            detail=error_reason or "Failed to restart sandbox",
        )

    await _sync_tenant_sandboxes_with_backend(tenant_id, [sandbox_id])
    if ip_address:
        sandbox.ipAddress = ip_address
    sandbox.updatedAt = datetime.utcnow()
    return sandbox


def _get_sandbox_or_404(tenant_id: str, sandbox_id: str) -> Sandbox:
    tenant_sandboxes = sandboxes.get(tenant_id, {})
    sandbox = tenant_sandboxes.get(sandbox_id)
    if sandbox is None:
        raise HTTPException(status_code=404, detail="Sandbox not found")
    return sandbox


async def _sync_tenant_sandboxes_with_backend(
    tenant_id: str, sandbox_ids: Optional[list[str]] = None
):
    """Update sandbox state from the active backend if available."""
    if SANDBOX_BACKEND != "kubernetes":
        return
    tenant_sandboxes = sandboxes.get(tenant_id)
    if not tenant_sandboxes:
        return

    ids_to_sync = sandbox_ids or list(tenant_sandboxes.keys())
    for sandbox_id in ids_to_sync:
        sandbox = tenant_sandboxes.get(sandbox_id)
        if sandbox is None:
            continue
        try:
            state, ip_address, error_reason = await kubernetes_manager.get_pod_status(
                sandbox_id
            )
        except Exception as exc:
            logger.error("Failed to sync sandbox %s status: %s", sandbox_id, exc)
            continue

        if state:
            sandbox.state = state
        elif error_reason:
            sandbox.state = SandboxState.error
        if ip_address:
            sandbox.ipAddress = ip_address
        sandbox.errorReason = error_reason
        sandbox.updatedAt = datetime.utcnow()



# Convenience for external import
def get_app() -> FastAPI:
    return app


from __future__ import annotations

import asyncio
from contextlib import asynccontextmanager
import logging
import uuid
from datetime import datetime
from typing import Optional
import os
import sys

import httpx
from fastapi import FastAPI, File, Form, HTTPException, Query, Security, Response, UploadFile
from fastapi.security import APIKeyHeader

from common.models.common import Error
from common.models.control_protocol import CreateSandboxRequest
from common.models.sandbox import (
    ExecuteCommandRequest,
    ExecuteCommandResponse,
    FileUploadResponse,
    SandboxState,
    Sandbox,
    CreateSandbox,
    SandboxList,
    UploadUrlRequest,
    UploadUrlResponse,
    ConfirmUploadRequest,
)
from services.isola_controller.agent_manager import AgentManager
from services.isola_controller.kubernetes_control.sandboxes import KubernetesManager
from common.storage import create_storage
from common.storage.base import ObjectStorage

log_level = os.getenv("LOG_LEVEL", "info").upper()

logger = logging.getLogger(__name__)
logger.setLevel(getattr(logging, log_level, logging.INFO))

# Add handler to this module's logger
handler = logging.StreamHandler(sys.stdout)
handler.setLevel(getattr(logging, log_level, logging.INFO))
formatter = logging.Formatter(
    fmt="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S",
)
handler.setFormatter(formatter)
logger.addHandler(handler)


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

# File upload configuration
# Files larger than this threshold will require signed URL upload (not implemented yet)
FILE_SIZE_THRESHOLD_BYTES = 5 * 1024 * 1024  # 5MB
AGENT_SIDECAR_PORT = 8080

kubernetes_manager = KubernetesManager(
    namespace=KUBERNETES_NAMESPACE,
    api_server_url=MINIKUBE_API_SERVER,
)

# Initialize storage for large file uploads
try:
    storage: Optional[ObjectStorage] = create_storage()
    logger.info("Storage initialized successfully")
except Exception as e:
    logger.warning(f"Failed to initialize storage: {e}. Large file uploads will not be available.")
    storage = None

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


async def get_sandbox_from_k8s(sandbox_id: str) -> Optional[Sandbox]:
    """
    Fetch sandbox details from Kubernetes and return a Sandbox object.
    Returns None if the sandbox is not found.
    """
    state_str, ip_address, error_reason, name = await kubernetes_manager.get_sandbox_cr_status(sandbox_id)
    if error_reason == "Sandbox not found":
        return None
    
    return Sandbox.model_validate({
        "id": sandbox_id,
        "name": f"sandbox-{sandbox_id[:8]}",
        "state": state,
        "desiredState": state,
        "env": {},
        "labels": {},
        "errorReason": error_reason,
        "createdAt": datetime.utcnow(),
    })



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
            sandbox_id = pod_data.get("sandbox_id")
            if not sandbox_id:
                continue
            
            sandbox = await get_sandbox_from_k8s(sandbox_id)
            if sandbox and (state is None or sandbox.state == state):
                items.append(sandbox)
    else:
        # For agent backend, return empty list for now
        items = []
    
    logger.info(f"There are {len(items)} sandboxes")
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
        "env": req.env or {},
        "labels": req.labels or {},
        "errorReason": None,
        "createdAt": now,
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
    """Provision the sandbox by creating a Sandbox CR (operator handles Pod creation)"""
    try:
        # Create Sandbox CR - the isola-operator will watch for this and create the Pod
        success, error_reason = await kubernetes_manager.create_sandbox_cr(
            sandbox_id=sandbox_id,
            name=request.name,
            template_name="default-template",  # Using the default template for now
        )

        logger.info(
            "Sandbox %s CR creation result success=%s reason=%s",
            sandbox_id,
            success,
            error_reason,
        )
    except Exception as exc:
        logger.error("Error creating sandbox %s via Kubernetes CR: %s", sandbox_id, exc)

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
        sandbox = await get_sandbox_from_k8s(sandbox_id)
        if sandbox is None:
            raise HTTPException(status_code=404, detail="Sandbox not found")
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
        # Delete the Sandbox CR - the operator will handle pod cleanup via owner references
        success, error_reason = await kubernetes_manager.delete_sandbox_cr(sandbox_id)
        if not success:
            status_code = 404 if error_reason and "not found" in error_reason.lower() else 500
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
    logger.info(f"[EXECUTE] Request for sandbox {sandbox_id}: {command_request.command}")
    
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
        logger.warning(f"[EXECUTE] Sandbox {sandbox_id} not found")
        raise HTTPException(status_code=404, detail="Sandbox not found")
    
    if state != SandboxState.running:
        logger.warning(f"[EXECUTE] Sandbox {sandbox_id} not in running state: {state}")
        raise HTTPException(
            status_code=409,
            detail=f"Sandbox must be in 'running' state, current state: {state}"
        )
    
    # Execute command in Kubernetes pod
    stdout, stderr, exit_code = await kubernetes_manager.execute_command(
        sandbox_id, 
        command_request.command
    )
    
    logger.info(f"[EXECUTE] Command completed for sandbox {sandbox_id}: exit_code={exit_code}")
    
    return ExecuteCommandResponse(
        stdout=stdout,
        stderr=stderr,
        exitCode=exit_code
    )


# ============================================================================
# File Upload Routes
# ============================================================================

@app.post(
    "/sandboxes/{sandbox_id}/files",
    response_model=FileUploadResponse,
    status_code=200,
    tags=["files"],
    summary="Upload a file to a sandbox",
    description="Upload a file to the specified sandbox. Files smaller than 5MB are uploaded directly. Larger files will require signed URL upload (not yet implemented).",
    operation_id="uploadFile",
    responses={
        200: {"description": "File uploaded successfully"},
        400: {"description": "Bad request - Invalid input", "model": Error},
        401: {"description": "Unauthorized - Invalid or missing API key", "model": Error},
        404: {"description": "Sandbox not found", "model": Error},
        409: {"description": "Conflict - Sandbox not in running state", "model": Error},
        501: {"description": "Not implemented - Large file upload requires signed URL", "model": Error},
    }
)
async def upload_file(
    sandbox_id: str,
    file: UploadFile = File(..., description="File to upload to the sandbox"),
    path: str = Form(..., description="Target path in the sandbox where the file should be written"),
    api_key: Optional[str] = Security(api_key_header),
):
    """
    Upload a file to a sandbox.
    
    Small files (< 5MB) are uploaded directly via the agent sidecar.
    Large files (>= 5MB) will return 501 - signed URL upload not yet implemented.
    """
    logger.info(f"[UPLOAD] Request for sandbox {sandbox_id}: path={path}")
    
    # Validate API key
    tenant_id = tenant_from_api_key(api_key)
    
    if SANDBOX_BACKEND != "kubernetes":
        raise HTTPException(
            status_code=501,
            detail="File upload is only implemented for the Kubernetes backend"
        )
    
    # Check sandbox exists and is running
    state, ip_address, error_reason = await kubernetes_manager.get_pod_status(sandbox_id)
    if state is None:
        logger.warning(f"[UPLOAD] Sandbox {sandbox_id} not found")
        raise HTTPException(status_code=404, detail="Sandbox not found")
    
    if state != SandboxState.running:
        logger.warning(f"[UPLOAD] Sandbox {sandbox_id} not in running state: {state}")
        raise HTTPException(
            status_code=409,
            detail=f"Sandbox must be in 'running' state, current state: {state}"
        )
    
    if not ip_address:
        logger.error(f"[UPLOAD] Sandbox {sandbox_id} has no IP address")
        raise HTTPException(
            status_code=500,
            detail="Sandbox pod has no IP address"
        )
    
    # Read file content to check size
    content = await file.read()
    file_size = len(content)
    
    logger.info(f"[UPLOAD] File size: {file_size} bytes, threshold: {FILE_SIZE_THRESHOLD_BYTES} bytes")
    
    # Check file size threshold
    if file_size >= FILE_SIZE_THRESHOLD_BYTES:
        logger.info(f"[UPLOAD] File too large for direct upload: {file_size} bytes")
        raise HTTPException(
            status_code=501,
            detail=f"File size ({file_size} bytes) exceeds direct upload limit ({FILE_SIZE_THRESHOLD_BYTES} bytes). Signed URL upload is not yet implemented."
        )
    
    # Forward the file to the agent sidecar
    agent_url = f"http://{ip_address}:{AGENT_SIDECAR_PORT}/upload"
    logger.info(f"[UPLOAD] Forwarding to agent at {agent_url}")
    
    try:
        async with httpx.AsyncClient(timeout=30.0) as client:
            # Prepare multipart form data
            files = {"file": (file.filename or "uploaded_file", content)}
            data = {"path": path}
            
            response = await client.post(agent_url, files=files, data=data)
            
            if response.status_code != 200:
                logger.error(
                    f"[UPLOAD] Agent returned error: {response.status_code} - {response.text}"
                )
                raise HTTPException(
                    status_code=response.status_code,
                    detail=f"Agent upload failed: {response.text}"
                )
            
            agent_response = response.json()
            logger.info(f"[UPLOAD] Successfully uploaded file to sandbox {sandbox_id}: {agent_response}")
            
            return FileUploadResponse(
                success=agent_response.get("success", True),
                path=agent_response.get("path", path),
                size=agent_response.get("size", file_size)
            )
            
    except httpx.TimeoutException:
        logger.error(f"[UPLOAD] Timeout connecting to agent at {agent_url}")
        raise HTTPException(
            status_code=504,
            detail="Timeout connecting to sandbox agent"
        )
    except httpx.ConnectError as e:
        logger.error(f"[UPLOAD] Failed to connect to agent at {agent_url}: {e}")
        raise HTTPException(
            status_code=502,
            detail=f"Failed to connect to sandbox agent: {str(e)}"
        )
    except HTTPException:
        raise
    except Exception as e:
        logger.exception(f"[UPLOAD] Unexpected error uploading to sandbox {sandbox_id}")
        raise HTTPException(
            status_code=500,
            detail=f"Unexpected error during upload: {str(e)}"
        )


@app.post(
    "/sandboxes/{sandbox_id}/files/upload-url",
    response_model=UploadUrlResponse,
    status_code=200,
    tags=["files"],
    summary="Generate presigned URL for large file upload",
    description="Generate a presigned URL for uploading large files directly to S3. The client should upload the file to the returned URL, then call the confirm endpoint.",
    operation_id="generateUploadUrl",
    responses={
        200: {"description": "Presigned URL generated successfully"},
        400: {"description": "Bad request - Invalid input", "model": Error},
        401: {"description": "Unauthorized - Invalid or missing API key", "model": Error},
        404: {"description": "Sandbox not found", "model": Error},
        409: {"description": "Sandbox not in running state", "model": Error},
        501: {"description": "Not implemented - Storage not configured", "model": Error},
    }
)
async def generate_upload_url(
    sandbox_id: str,
    request: UploadUrlRequest,
    api_key: Optional[str] = Security(api_key_header),
):
    """
    Generate a presigned URL for uploading large files directly to S3.
    
    The client should:
    1. Upload the file to the returned presigned URL using PUT
    2. Call the confirm endpoint with the upload_id to trigger agent download
    """
    logger.info(f"[UPLOAD-URL] Request for sandbox {sandbox_id}: path={request.path}, filename={request.filename}")
    
    # Validate API key
    tenant_id = tenant_from_api_key(api_key)
    
    if storage is None:
        raise HTTPException(
            status_code=501,
            detail="Storage not configured. Large file uploads are not available."
        )
    
    if SANDBOX_BACKEND != "kubernetes":
        raise HTTPException(
            status_code=501,
            detail="Large file upload is only implemented for the Kubernetes backend"
        )
    
    # Check sandbox exists and is running
    state, ip_address, error_reason = await kubernetes_manager.get_pod_status(sandbox_id)
    if state is None:
        logger.warning(f"[UPLOAD-URL] Sandbox {sandbox_id} not found")
        raise HTTPException(status_code=404, detail="Sandbox not found")
    
    if state != SandboxState.running:
        logger.warning(f"[UPLOAD-URL] Sandbox {sandbox_id} not in running state: {state}")
        raise HTTPException(
            status_code=409,
            detail=f"Sandbox must be in 'running' state, current state: {state}"
        )
    
    # Generate unique upload ID and S3 key
    upload_id = str(uuid.uuid4())
    s3_key = f"uploads/{tenant_id}/{sandbox_id}/{upload_id}/{request.filename}"
    
    # Generate presigned upload URL (15 minutes expiration)
    expires_in = 900  # 15 minutes
    try:
        upload_url = await storage.generate_presigned_upload_url(
            key=s3_key,
            expires_in=expires_in,
            content_type=request.content_type,
        )
    except Exception as e:
        logger.error(f"[UPLOAD-URL] Failed to generate presigned URL: {e}")
        raise HTTPException(
            status_code=500,
            detail=f"Failed to generate presigned URL: {str(e)}"
        )
    
    logger.info(f"[UPLOAD-URL] Generated presigned URL for upload_id={upload_id}, s3_key={s3_key}")
    
    return UploadUrlResponse(
        upload_url=upload_url,
        upload_id=upload_id,
        expires_in=expires_in,
    )


@app.post(
    "/sandboxes/{sandbox_id}/files/confirm",
    status_code=200,
    tags=["files"],
    summary="Confirm upload and trigger agent download",
    description="Confirm that a file has been uploaded to S3 and trigger the agent to download it to the sandbox.",
    operation_id="confirmUpload",
    responses={
        200: {"description": "Upload confirmed and agent download triggered"},
        400: {"description": "Bad request - Invalid input or upload not found", "model": Error},
        401: {"description": "Unauthorized - Invalid or missing API key", "model": Error},
        404: {"description": "Sandbox not found or upload not found", "model": Error},
        409: {"description": "Conflict - Sandbox not in running state", "model": Error},
        501: {"description": "Not implemented - Storage not configured", "model": Error},
    }
)
async def confirm_upload(
    sandbox_id: str,
    request: ConfirmUploadRequest,
    api_key: Optional[str] = Security(api_key_header),
):
    """
    Confirm that a file has been uploaded to S3 and trigger the agent to download it.
    
    This endpoint:
    1. Reconstructs the S3 key from upload_id and filename
    2. Generates a presigned download URL
    3. Calls the agent's /download endpoint to download and write the file
    4. The agent deletes the file from S3 after successful download
    """
    logger.info(f"[CONFIRM] Request for sandbox {sandbox_id}: upload_id={request.upload_id}, filename={request.filename}, path={request.path}")
    
    # Validate API key
    tenant_id = tenant_from_api_key(api_key)
    
    if storage is None:
        raise HTTPException(
            status_code=501,
            detail="Storage not configured. Large file uploads are not available."
        )
    
    if SANDBOX_BACKEND != "kubernetes":
        raise HTTPException(
            status_code=501,
            detail="Large file upload is only implemented for the Kubernetes backend"
        )
    
    # Check sandbox exists and is running
    state, ip_address, error_reason = await kubernetes_manager.get_pod_status(sandbox_id)
    if state is None:
        logger.warning(f"[CONFIRM] Sandbox {sandbox_id} not found")
        raise HTTPException(status_code=404, detail="Sandbox not found")
    
    if state != SandboxState.running:
        logger.warning(f"[CONFIRM] Sandbox {sandbox_id} not in running state: {state}")
        raise HTTPException(
            status_code=409,
            detail=f"Sandbox must be in 'running' state, current state: {state}"
        )
    
    if not ip_address:
        logger.error(f"[CONFIRM] Sandbox {sandbox_id} has no IP address")
        raise HTTPException(
            status_code=500,
            detail="Sandbox pod has no IP address"
        )
    
    # Reconstruct S3 key from upload_id and filename (must match upload-url endpoint)
    s3_key = f"uploads/{tenant_id}/{sandbox_id}/{request.upload_id}/{request.filename}"
    target_path = request.path
    
    # Generate presigned download URL for the agent
    try:
        download_url = await storage.generate_presigned_download_url(
            key=s3_key,
            expires_in=900,  # 15 minutes
        )
    except Exception as e:
        logger.error(f"[CONFIRM] Failed to generate presigned download URL: {e}")
        raise HTTPException(
            status_code=500,
            detail=f"Failed to generate download URL: {str(e)}"
        )
    
    # Call agent's /download endpoint
    agent_url = f"http://{ip_address}:{AGENT_SIDECAR_PORT}/download"
    logger.info(f"[CONFIRM] Triggering agent download at {agent_url}")
    
    try:
        async with httpx.AsyncClient(timeout=60.0) as client:
            # Send download request to agent
            # The agent will download from S3, write to shared volume, and delete from S3
            response = await client.post(
                agent_url,
                json={
                    "download_url": download_url,
                    "path": target_path,
                    "delete_after": True,
                }
            )
            
            if response.status_code != 200:
                logger.error(
                    f"[CONFIRM] Agent returned error: {response.status_code} - {response.text}"
                )
                raise HTTPException(
                    status_code=response.status_code,
                    detail=f"Agent download failed: {response.text}"
                )
            
            agent_response = response.json()
            logger.info(f"[CONFIRM] Successfully triggered download for sandbox {sandbox_id}: {agent_response}")
            
            return {
                "success": True,
                "path": agent_response.get("path", target_path),
                "size": agent_response.get("size", 0),
            }
            
    except httpx.TimeoutException:
        logger.error(f"[CONFIRM] Timeout connecting to agent at {agent_url}")
        raise HTTPException(
            status_code=504,
            detail="Timeout connecting to sandbox agent"
        )
    except httpx.ConnectError as e:
        logger.error(f"[CONFIRM] Failed to connect to agent at {agent_url}: {e}")
        raise HTTPException(
            status_code=502,
            detail=f"Failed to connect to sandbox agent: {str(e)}"
        )
    except HTTPException:
        raise
    except Exception as e:
        logger.exception(f"[CONFIRM] Unexpected error confirming upload for sandbox {sandbox_id}")
        raise HTTPException(
            status_code=500,
            detail=f"Unexpected error during upload confirmation: {str(e)}"
        )


# Convenience for external import
def get_app() -> FastAPI:
    return app

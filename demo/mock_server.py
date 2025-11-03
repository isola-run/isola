"""
Mock Isola API Server
Implements a subset of the Isola API for demonstration purposes
"""
import asyncio
import subprocess
import sys
import tempfile
import uuid
from datetime import datetime
from typing import Dict, List, Optional, Any
from enum import Enum
import shutil
import os
from pathlib import Path

from fastapi import FastAPI, HTTPException, Header, Query, BackgroundTasks
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel, Field
import uvicorn


# Enums
class SandboxState(str, Enum):
    CREATING = "creating"
    STARTING = "starting"
    STARTED = "started"
    STOPPING = "stopping"
    STOPPED = "stopped"
    DESTROYING = "destroying"
    DESTROYED = "destroyed"
    ERROR = "error"
    UNKNOWN = "unknown"


class SandboxClass(str, Enum):
    SMALL = "small"
    MEDIUM = "medium"
    LARGE = "large"
    XLARGE = "xlarge"


# Models
class HealthStatus(BaseModel):
    status: str
    timestamp: datetime
    components: Dict[str, bool] = {
        "database": True,
        "runners": True,
        "storage": True
    }


class SystemConfig(BaseModel):
    version: str = "1.0.0"
    defaultSnapshot: str = "python:3.11"
    sshGatewayHost: str = "localhost"
    sshGatewayPort: int = 22
    maxSandboxes: int = 10
    maxVolumes: int = 50
    regions: List[str] = ["local"]


class CreateSandbox(BaseModel):
    name: str
    snapshot: Optional[str] = "python:3.11"
    class_: Optional[SandboxClass] = Field(default=SandboxClass.SMALL, alias="class")
    region: Optional[str] = "local"
    cpu: Optional[int] = 1
    memory: Optional[int] = 1
    disk: Optional[int] = 10
    gpu: Optional[int] = 0
    env: Optional[Dict[str, str]] = {}
    labels: Optional[Dict[str, str]] = {}
    autoStart: Optional[bool] = True


class Sandbox(BaseModel):
    id: str
    name: str
    state: SandboxState
    desiredState: Optional[SandboxState] = None
    class_: SandboxClass = Field(alias="class")
    region: str
    snapshot: str
    cpu: int
    memory: int
    disk: int
    gpu: int
    env: Dict[str, str]
    labels: Dict[str, str]
    ipAddress: Optional[str] = None
    errorReason: Optional[str] = None
    createdAt: datetime
    updatedAt: datetime
    lastActivityAt: Optional[datetime] = None


class SandboxList(BaseModel):
    items: List[Sandbox]
    total: int
    limit: int
    offset: int


class CodeExecutionRequest(BaseModel):
    code: str
    language: str = "python"
    timeout: Optional[int] = 30


class CodeExecutionResponse(BaseModel):
    output: str
    error: Optional[str] = None
    exitCode: int
    executionTime: float


# In-memory storage
class SandboxStore:
    def __init__(self):
        self.sandboxes: Dict[str, Sandbox] = {}
        self.sandbox_processes: Dict[str, Any] = {}
        self.sandbox_dirs: Dict[str, Path] = {}
        
    def create_sandbox(self, create_req: CreateSandbox) -> Sandbox:
        sandbox_id = str(uuid.uuid4())
        now = datetime.utcnow()
        
        # Access the class field properly
        sandbox_class = create_req.class_ if hasattr(create_req, 'class_') else SandboxClass.SMALL
        
        sandbox = Sandbox(
            id=sandbox_id,
            name=create_req.name,
            state=SandboxState.CREATING,
            **{"class": sandbox_class},  # Use the actual field name, not the alias
            region=create_req.region or "local",
            snapshot=create_req.snapshot or "python:3.11",
            cpu=create_req.cpu or 1,
            memory=create_req.memory or 1,
            disk=create_req.disk or 10,
            gpu=create_req.gpu or 0,
            env=create_req.env or {},
            labels=create_req.labels or {},
            ipAddress=f"10.0.0.{len(self.sandboxes) + 1}",
            createdAt=now,
            updatedAt=now
        )
        
        # Create a temporary directory for this sandbox
        sandbox_dir = Path(tempfile.mkdtemp(prefix=f"isola_sandbox_{sandbox_id}_"))
        self.sandbox_dirs[sandbox_id] = sandbox_dir
        
        # Create a simple Python environment file for execution
        (sandbox_dir / "executor.py").write_text("""
import sys
import io
import contextlib

code = sys.argv[1]

# Capture output
output = io.StringIO()
with contextlib.redirect_stdout(output), contextlib.redirect_stderr(output):
    try:
        exec(code)
    except Exception as e:
        print(f"Error: {e}", file=output)

print(output.getvalue())
""")
        
        self.sandboxes[sandbox_id] = sandbox
        
        # Check autoStart flag
        auto_start = create_req.autoStart if create_req.autoStart is not None else True
        
        if auto_start:
            sandbox.state = SandboxState.STARTING
            # Simulate async startup
            sandbox.state = SandboxState.STARTED
            sandbox.updatedAt = datetime.utcnow()
        else:
            sandbox.state = SandboxState.STOPPED
            
        return sandbox
    
    def get_sandbox(self, sandbox_id: str) -> Optional[Sandbox]:
        return self.sandboxes.get(sandbox_id)
    
    def delete_sandbox(self, sandbox_id: str) -> bool:
        if sandbox_id in self.sandboxes:
            # Clean up sandbox directory
            if sandbox_id in self.sandbox_dirs:
                sandbox_dir = self.sandbox_dirs[sandbox_id]
                if sandbox_dir.exists():
                    shutil.rmtree(sandbox_dir)
                del self.sandbox_dirs[sandbox_id]
            
            del self.sandboxes[sandbox_id]
            if sandbox_id in self.sandbox_processes:
                del self.sandbox_processes[sandbox_id]
            return True
        return False
    
    def list_sandboxes(self, state: Optional[SandboxState] = None, 
                      limit: int = 20, offset: int = 0) -> SandboxList:
        sandboxes = list(self.sandboxes.values())
        
        if state:
            sandboxes = [s for s in sandboxes if s.state == state]
        
        total = len(sandboxes)
        sandboxes = sandboxes[offset:offset + limit]
        
        return SandboxList(
            items=sandboxes,
            total=total,
            limit=limit,
            offset=offset
        )
    
    def start_sandbox(self, sandbox_id: str) -> Optional[Sandbox]:
        sandbox = self.get_sandbox(sandbox_id)
        if not sandbox:
            return None
            
        if sandbox.state not in [SandboxState.STOPPED, SandboxState.CREATED]:
            raise HTTPException(status_code=409, detail="Sandbox must be stopped to start")
            
        sandbox.state = SandboxState.STARTING
        sandbox.updatedAt = datetime.utcnow()
        
        # Simulate startup
        sandbox.state = SandboxState.STARTED
        sandbox.lastActivityAt = datetime.utcnow()
        
        return sandbox
    
    def stop_sandbox(self, sandbox_id: str) -> Optional[Sandbox]:
        sandbox = self.get_sandbox(sandbox_id)
        if not sandbox:
            return None
            
        if sandbox.state != SandboxState.STARTED:
            raise HTTPException(status_code=409, detail="Sandbox must be running to stop")
            
        sandbox.state = SandboxState.STOPPING
        sandbox.updatedAt = datetime.utcnow()
        
        # Simulate shutdown
        sandbox.state = SandboxState.STOPPED
        
        return sandbox
    
    def execute_code(self, sandbox_id: str, request: CodeExecutionRequest) -> CodeExecutionResponse:
        sandbox = self.get_sandbox(sandbox_id)
        if not sandbox:
            raise HTTPException(status_code=404, detail="Sandbox not found")
            
        if sandbox.state != SandboxState.STARTED:
            raise HTTPException(status_code=400, detail="Sandbox must be running to execute code")
        
        # Update activity
        sandbox.lastActivityAt = datetime.utcnow()
        
        # Get sandbox directory
        sandbox_dir = self.sandbox_dirs.get(sandbox_id)
        if not sandbox_dir or not sandbox_dir.exists():
            raise HTTPException(status_code=500, detail="Sandbox directory not found")
        
        import time
        start_time = time.time()
        
        try:
            # Execute code based on language
            if request.language == "python":
                result = subprocess.run(
                    [sys.executable, str(sandbox_dir / "executor.py"), request.code],
                    capture_output=True,
                    text=True,
                    timeout=request.timeout,
                    cwd=str(sandbox_dir)
                )
                
                execution_time = time.time() - start_time
                
                return CodeExecutionResponse(
                    output=result.stdout,
                    error=result.stderr if result.stderr else None,
                    exitCode=result.returncode,
                    executionTime=execution_time
                )
                
            elif request.language == "bash":
                result = subprocess.run(
                    ["bash", "-c", request.code],
                    capture_output=True,
                    text=True,
                    timeout=request.timeout,
                    cwd=str(sandbox_dir)
                )
                
                execution_time = time.time() - start_time
                
                return CodeExecutionResponse(
                    output=result.stdout,
                    error=result.stderr if result.stderr else None,
                    exitCode=result.returncode,
                    executionTime=execution_time
                )
            else:
                raise HTTPException(status_code=400, detail=f"Unsupported language: {request.language}")
                
        except subprocess.TimeoutExpired:
            execution_time = time.time() - start_time
            return CodeExecutionResponse(
                output="",
                error="Execution timed out",
                exitCode=-1,
                executionTime=execution_time
            )
        except Exception as e:
            execution_time = time.time() - start_time
            return CodeExecutionResponse(
                output="",
                error=str(e),
                exitCode=-1,
                executionTime=execution_time
            )


# Initialize app and storage
app = FastAPI(title="Isola Mock Server", version="1.0.0")
store = SandboxStore()

# Add CORS middleware
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)


# API Key validation
def validate_api_key(x_api_key: Optional[str] = Header(None)):
    # For demo purposes, accept any key or no key
    return True


# Routes
@app.get("/health", response_model=HealthStatus)
async def get_health():
    """Health check endpoint"""
    return HealthStatus(
        status="healthy",
        timestamp=datetime.utcnow()
    )


@app.get("/config", response_model=SystemConfig)
async def get_config(x_api_key: Optional[str] = Header(None)):
    """Get system configuration"""
    validate_api_key(x_api_key)
    return SystemConfig()


@app.post("/sandboxes", response_model=Sandbox, status_code=201)
async def create_sandbox(
    request: CreateSandbox, 
    background_tasks: BackgroundTasks,
    x_api_key: Optional[str] = Header(None)
):
    """Create a new sandbox"""
    validate_api_key(x_api_key)
    
    # Check if name already exists
    if any(s.name == request.name for s in store.sandboxes.values()):
        raise HTTPException(status_code=409, detail="Sandbox name already exists")
    
    sandbox = store.create_sandbox(request)
    return sandbox


@app.get("/sandboxes", response_model=SandboxList)
async def list_sandboxes(
    state: Optional[SandboxState] = None,
    limit: int = Query(default=20, le=100),
    offset: int = Query(default=0, ge=0),
    x_api_key: Optional[str] = Header(None)
):
    """List all sandboxes"""
    validate_api_key(x_api_key)
    return store.list_sandboxes(state, limit, offset)


@app.get("/sandboxes/{sandbox_id}", response_model=Sandbox)
async def get_sandbox(sandbox_id: str, x_api_key: Optional[str] = Header(None)):
    """Get sandbox details"""
    validate_api_key(x_api_key)
    sandbox = store.get_sandbox(sandbox_id)
    if not sandbox:
        raise HTTPException(status_code=404, detail="Sandbox not found")
    return sandbox


@app.delete("/sandboxes/{sandbox_id}", status_code=204)
async def delete_sandbox(
    sandbox_id: str, 
    force: bool = Query(default=False),
    x_api_key: Optional[str] = Header(None)
):
    """Delete a sandbox"""
    validate_api_key(x_api_key)
    sandbox = store.get_sandbox(sandbox_id)
    if not sandbox:
        raise HTTPException(status_code=404, detail="Sandbox not found")
    
    if sandbox.state == SandboxState.STARTED and not force:
        raise HTTPException(status_code=409, detail="Cannot delete running sandbox without force=true")
    
    if not store.delete_sandbox(sandbox_id):
        raise HTTPException(status_code=404, detail="Sandbox not found")
    return None


@app.post("/sandboxes/{sandbox_id}/start", response_model=Sandbox)
async def start_sandbox(sandbox_id: str, x_api_key: Optional[str] = Header(None)):
    """Start a sandbox"""
    validate_api_key(x_api_key)
    sandbox = store.start_sandbox(sandbox_id)
    if not sandbox:
        raise HTTPException(status_code=404, detail="Sandbox not found")
    return sandbox


@app.post("/sandboxes/{sandbox_id}/stop", response_model=Sandbox)
async def stop_sandbox(sandbox_id: str, x_api_key: Optional[str] = Header(None)):
    """Stop a sandbox"""
    validate_api_key(x_api_key)
    sandbox = store.stop_sandbox(sandbox_id)
    if not sandbox:
        raise HTTPException(status_code=404, detail="Sandbox not found")
    return sandbox


@app.post("/sandboxes/{sandbox_id}/restart", response_model=Sandbox)
async def restart_sandbox(sandbox_id: str, x_api_key: Optional[str] = Header(None)):
    """Restart a sandbox"""
    validate_api_key(x_api_key)
    sandbox = store.get_sandbox(sandbox_id)
    if not sandbox:
        raise HTTPException(status_code=404, detail="Sandbox not found")
    
    if sandbox.state == SandboxState.STARTED:
        store.stop_sandbox(sandbox_id)
    
    sandbox = store.start_sandbox(sandbox_id)
    return sandbox


# Extended API for code execution (not in original spec but useful for demo)
@app.post("/sandboxes/{sandbox_id}/execute", response_model=CodeExecutionResponse)
async def execute_code(
    sandbox_id: str, 
    request: CodeExecutionRequest,
    x_api_key: Optional[str] = Header(None)
):
    """Execute code in a sandbox"""
    validate_api_key(x_api_key)
    return store.execute_code(sandbox_id, request)


# Cleanup on shutdown
@app.on_event("shutdown")
async def shutdown_event():
    """Clean up all sandbox directories on shutdown"""
    for sandbox_dir in store.sandbox_dirs.values():
        if sandbox_dir.exists():
            shutil.rmtree(sandbox_dir)


if __name__ == "__main__":
    import sys
    print("Starting Isola Mock Server...")
    print("API Documentation: http://localhost:3000/docs")
    uvicorn.run(app, host="0.0.0.0", port=3000)

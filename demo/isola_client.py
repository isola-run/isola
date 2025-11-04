"""
Isola API Client
A Python client for interacting with the Isola Sandbox Infrastructure API
"""
import json
import time
from typing import Dict, List, Optional, Any
from dataclasses import dataclass
from enum import Enum

import requests
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry


class SandboxState(str, Enum):
    """Sandbox state enum"""
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
    """Sandbox size class enum"""
    SMALL = "small"
    MEDIUM = "medium"
    LARGE = "large"
    XLARGE = "xlarge"


@dataclass
class SandboxConfig:
    """Configuration for creating a sandbox"""
    name: str
    image: str = "python:3.11"
    sandbox_class: SandboxClass = SandboxClass.SMALL
    region: str = "local"
    cpu: int = 1
    memory: int = 1  # GB
    disk: int = 10  # GB
    gpu: int = 0
    env: Dict[str, str] = None
    labels: Dict[str, str] = None
    auto_start: bool = True

    def to_dict(self):
        """Convert to dictionary for API request"""
        data = {
            "name": self.name,
            "image": self.image,
            "class": self.sandbox_class,
            "region": self.region,
            "cpu": self.cpu,
            "memory": self.memory,
            "disk": self.disk,
            "gpu": self.gpu,
            "autoStart": self.auto_start
        }
        if self.env:
            data["env"] = self.env
        if self.labels:
            data["labels"] = self.labels
        return data


class IsolaClient:
    """Client for interacting with the Isola API"""
    
    def __init__(self, base_url: str = "http://localhost:3000", api_key: Optional[str] = None):
        """
        Initialize the Isola client
        
        Args:
            base_url: Base URL of the Isola API server
            api_key: Optional API key for authentication
        """
        self.base_url = base_url.rstrip('/')
        self.api_key = api_key
        
        # Setup session with retry logic
        self.session = requests.Session()
        retry = Retry(
            total=3,
            backoff_factor=0.3,
            status_forcelist=[500, 502, 503, 504]
        )
        adapter = HTTPAdapter(max_retries=retry)
        self.session.mount('http://', adapter)
        self.session.mount('https://', adapter)
        
        # Set headers
        self.headers = {"Content-Type": "application/json"}
        if api_key:
            self.headers["X-API-Key"] = api_key
    
    def _request(self, method: str, endpoint: str, **kwargs) -> requests.Response:
        """Make a request to the API"""
        url = f"{self.base_url}{endpoint}"
        kwargs.setdefault("headers", {}).update(self.headers)
        
        response = self.session.request(method, url, **kwargs)
        response.raise_for_status()
        return response
    
    # Health and Configuration
    def health_check(self) -> Dict[str, Any]:
        """Check the health of the API"""
        response = self._request("GET", "/health")
        return response.json()
    
    def get_config(self) -> Dict[str, Any]:
        """Get system configuration"""
        response = self._request("GET", "/config")
        return response.json()
    
    # Sandbox Management
    def create_sandbox(self, config: SandboxConfig) -> Dict[str, Any]:
        """
        Create a new sandbox
        
        Args:
            config: Sandbox configuration
            
        Returns:
            Created sandbox details
        """
        response = self._request("POST", "/sandboxes", json=config.to_dict())
        return response.json()
    
    def list_sandboxes(self, state: Optional[SandboxState] = None, 
                      limit: int = 20, offset: int = 0) -> Dict[str, Any]:
        """
        List sandboxes
        
        Args:
            state: Optional filter by state
            limit: Maximum number of results
            offset: Number of results to skip
            
        Returns:
            List of sandboxes
        """
        params = {"limit": limit, "offset": offset}
        if state:
            params["state"] = state
        
        response = self._request("GET", "/sandboxes", params=params)
        return response.json()
    
    def get_sandbox(self, sandbox_id: str) -> Dict[str, Any]:
        """
        Get sandbox details
        
        Args:
            sandbox_id: Sandbox ID
            
        Returns:
            Sandbox details
        """
        response = self._request("GET", f"/sandboxes/{sandbox_id}")
        return response.json()
    
    def delete_sandbox(self, sandbox_id: str, force: bool = True) -> None:
        """
        Delete a sandbox
        
        Args:
            sandbox_id: Sandbox ID
            force: Force deletion even if running
        """
        params = {"force": force}
        self._request("DELETE", f"/sandboxes/{sandbox_id}", params=params)
    
    def start_sandbox(self, sandbox_id: str) -> Dict[str, Any]:
        """
        Start a sandbox
        
        Args:
            sandbox_id: Sandbox ID
            
        Returns:
            Updated sandbox details
        """
        response = self._request("POST", f"/sandboxes/{sandbox_id}/start")
        return response.json()
    
    def stop_sandbox(self, sandbox_id: str) -> Dict[str, Any]:
        """
        Stop a sandbox
        
        Args:
            sandbox_id: Sandbox ID
            
        Returns:
            Updated sandbox details
        """
        response = self._request("POST", f"/sandboxes/{sandbox_id}/stop")
        return response.json()
    
    def restart_sandbox(self, sandbox_id: str) -> Dict[str, Any]:
        """
        Restart a sandbox
        
        Args:
            sandbox_id: Sandbox ID
            
        Returns:
            Updated sandbox details
        """
        response = self._request("POST", f"/sandboxes/{sandbox_id}/restart")
        return response.json()
    
    def wait_for_sandbox(self, sandbox_id: str, target_state: SandboxState, 
                        timeout: int = 60, poll_interval: int = 2) -> bool:
        """
        Wait for a sandbox to reach a specific state
        
        Args:
            sandbox_id: Sandbox ID
            target_state: Target state to wait for
            timeout: Maximum time to wait in seconds
            poll_interval: Time between status checks in seconds
            
        Returns:
            True if target state reached, False if timeout
        """
        start_time = time.time()
        
        while time.time() - start_time < timeout:
            try:
                sandbox = self.get_sandbox(sandbox_id)
                if sandbox["state"] == target_state:
                    return True
                elif sandbox["state"] == SandboxState.ERROR:
                    raise Exception(f"Sandbox entered error state: {sandbox.get('errorReason', 'Unknown error')}")
            except Exception as e:
                if "404" not in str(e):
                    raise
            
            time.sleep(poll_interval)
        
        return False
    
    # Code Execution (Extended API)
    def execute_code(self, sandbox_id: str, code: str, language: str = "python", 
                     timeout: int = 30) -> Dict[str, Any]:
        """
        Execute code in a sandbox
        
        Args:
            sandbox_id: Sandbox ID
            code: Code to execute
            language: Programming language (python or bash)
            timeout: Execution timeout in seconds
            
        Returns:
            Execution result with stdout, stderr and exit code
        """
        payload = {
            "code": code,
            "language": language,
            "timeout": timeout
        }
        
        response = self._request("POST", f"/sandboxes/{sandbox_id}/execute", json=payload)
        return response.json()
    
    def execute_python(self, sandbox_id: str, code: str, timeout: int = 30) -> Dict[str, Any]:
        """
        Execute Python code in a sandbox
        
        Args:
            sandbox_id: Sandbox ID
            code: Python code to execute
            timeout: Execution timeout in seconds
            
        Returns:
            Execution result
        """
        return self.execute_code(sandbox_id, code, "python", timeout)
    
    def execute_bash(self, sandbox_id: str, command: str, timeout: int = 30) -> Dict[str, Any]:
        """
        Execute a bash command in a sandbox
        
        Args:
            sandbox_id: Sandbox ID
            command: Bash command to execute
            timeout: Execution timeout in seconds
            
        Returns:
            Execution result
        """
        return self.execute_code(sandbox_id, command, "bash", timeout)
    
    # Context Manager Support
    def sandbox(self, config: SandboxConfig):
        """
        Context manager for creating and managing a sandbox
        
        Usage:
            with client.sandbox(config) as sandbox:
                # Use sandbox
                pass
        """
        return SandboxContext(self, config)


class SandboxContext:
    """Context manager for sandbox lifecycle management"""
    
    def __init__(self, client: IsolaClient, config: SandboxConfig):
        self.client = client
        self.config = config
        self.sandbox = None
        self.sandbox_id = None
    
    def __enter__(self):
        """Create and start the sandbox"""
        try:
            # Create sandbox
            self.sandbox = self.client.create_sandbox(self.config)
            self.sandbox_id = self.sandbox["id"]
            
            # Wait for it to be ready if auto_start is enabled
            if self.config.auto_start:
                if self.client.wait_for_sandbox(self.sandbox_id, SandboxState.STARTED):
                    return self
                else:
                    raise Exception("Sandbox failed to start within timeout")
            
            return self
            
        except Exception as e:
            # Cleanup on error
            if self.sandbox_id:
                try:
                    self.client.delete_sandbox(self.sandbox_id, force=True)
                except:
                    pass
            raise
    
    def __exit__(self, exc_type, exc_val, exc_tb):
        """Stop and delete the sandbox"""
        if self.sandbox_id:
            try:
                # Stop if running
                sandbox = self.client.get_sandbox(self.sandbox_id)
                if sandbox["state"] == SandboxState.STARTED:
                    self.client.stop_sandbox(self.sandbox_id)
                    self.client.wait_for_sandbox(self.sandbox_id, SandboxState.STOPPED, timeout=30)
                
                # Delete sandbox
                self.client.delete_sandbox(self.sandbox_id, force=True)
            except Exception as e:
                print(f"Warning: Failed to cleanup sandbox {self.sandbox_id}: {e}")
    
    def execute_python(self, code: str, timeout: int = 30) -> Dict[str, Any]:
        """Execute Python code in the sandbox"""
        if not self.sandbox_id:
            raise Exception("Sandbox not initialized")
        return self.client.execute_python(self.sandbox_id, code, timeout)
    
    def execute_bash(self, command: str, timeout: int = 30) -> Dict[str, Any]:
        """Execute a bash command in the sandbox"""
        if not self.sandbox_id:
            raise Exception("Sandbox not initialized")
        return self.client.execute_bash(self.sandbox_id, command, timeout)
    
    def get_info(self) -> Dict[str, Any]:
        """Get current sandbox information"""
        if not self.sandbox_id:
            raise Exception("Sandbox not initialized")
        return self.client.get_sandbox(self.sandbox_id)

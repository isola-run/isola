"""
Integration tests for sandbox management API
Tests the full flow of creating, listing, and retrieving sandboxes
"""
import time
import pytest
import requests

from common.models.sandbox import SandboxState


@pytest.fixture(scope="module")
def api_client(base_url, api_key):
    """Fixture providing configured API client"""
    session = requests.Session()
    session.headers.update({
        "X-API-Key": api_key,
        "Content-Type": "application/json"
    })
    session.base_url = base_url
    return session


@pytest.fixture
def sandbox_data():
    """Default sandbox creation data"""
    return {
        "name": "test-sandbox",
        "image": "python:3.11"
    }


@pytest.fixture
def created_sandbox(api_client, sandbox_data):
    """Fixture that creates a sandbox and cleans it up after test"""
    # Create sandbox
    response = api_client.post(
        f"{api_client.base_url}/sandboxes",
        json=sandbox_data
    )
    assert response.status_code == 201
    sandbox = response.json()
    
    yield sandbox
    
    # Cleanup: Delete sandbox after test
    try:
        api_client.delete(
            f"{api_client.base_url}/sandboxes/{sandbox['id']}",
            params={"force": "true"}
        )
    except Exception:
        pass  # Ignore cleanup errors


class TestSandboxIntegration:
    """Integration tests for sandbox management"""
    
    def test_health_check(self, api_client):
        """Test that the health endpoint is accessible"""
        response = api_client.get(f"{api_client.base_url}/health")
        assert response.status_code == 200
        data = response.json()
        assert data["status"] == "healthy"
        assert "timestamp" in data
        assert "components" in data
        assert "version" in data
        assert "agent_count" in data
        
        # Verify components structure
        components = data["components"]
        assert "api" in components
        assert "agent_manager" in components
        assert "websocket_server" in components
    
    def test_create_sandbox(self, api_client, sandbox_data):
        """Test creating a new sandbox"""
        # Create sandbox
        response = api_client.post(
            f"{api_client.base_url}/sandboxes",
            json=sandbox_data
        )
        
        # Verify response
        assert response.status_code == 201
        sandbox = response.json()
        
        # Verify sandbox properties
        assert sandbox["name"] == sandbox_data["name"]
        assert sandbox["image"] == sandbox_data["image"]
        assert sandbox["state"] == SandboxState.pending.value
        assert sandbox["desiredState"] == SandboxState.running.value
        assert "id" in sandbox
        assert "createdAt" in sandbox
        assert "updatedAt" in sandbox
        
        # Store sandbox_id for cleanup
        sandbox_id = sandbox["id"]
        
        # Cleanup
        api_client.delete(f"{api_client.base_url}/sandboxes/{sandbox_id}", params={"force": "true"})
    
    def test_list_sandboxes(self, api_client, created_sandbox):
        """Test listing sandboxes"""
        # List sandboxes
        response = api_client.get(f"{api_client.base_url}/sandboxes")
        
        # Verify response
        assert response.status_code == 200
        data = response.json()
        
        # Verify structure
        assert "items" in data
        assert "total" in data
        assert "limit" in data
        assert "offset" in data
        
        # Verify our sandbox is in the list
        sandbox_ids = [s["id"] for s in data["items"]]
        assert created_sandbox["id"] in sandbox_ids
        
        # Find our sandbox and verify it
        our_sandbox = next(s for s in data["items"] if s["id"] == created_sandbox["id"])
        assert our_sandbox["name"] == created_sandbox["name"]
    
    def test_get_sandbox_by_id(self, api_client, created_sandbox):
        """Test retrieving a specific sandbox by ID"""
        sandbox_id = created_sandbox["id"]
        
        # Get sandbox
        response = api_client.get(f"{api_client.base_url}/sandboxes/{sandbox_id}")
        
        # Verify response
        assert response.status_code == 200
        sandbox = response.json()
        
        # Verify sandbox details
        assert sandbox["id"] == sandbox_id
        assert sandbox["name"] == created_sandbox["name"]
        assert sandbox["image"] == created_sandbox["image"]
        
    def test_sandbox_state_transition(self, api_client, created_sandbox):
        """Test that sandbox transitions from pending to running state"""
        sandbox_id = created_sandbox["id"]
        
        # Poll for state change (with timeout)
        max_attempts = 10
        for attempt in range(max_attempts):
            response = api_client.get(f"{api_client.base_url}/sandboxes/{sandbox_id}")
            assert response.status_code == 200
            
            sandbox = response.json()
            if sandbox["state"] == SandboxState.running.value:
                break
            elif sandbox["state"] == SandboxState.error.value:
                pytest.fail(f"Sandbox entered error state: {sandbox.get('errorReason')}")
            
            time.sleep(0.5)  # Wait before next attempt
        else:
            pytest.fail(f"Sandbox did not reach 'running' state after {max_attempts} attempts")
        
        # Verify final state
        assert sandbox["state"] == SandboxState.running.value
        assert sandbox["desiredState"] == SandboxState.running.value
    
    def test_get_nonexistent_sandbox(self, api_client):
        """Test retrieving a sandbox that doesn't exist"""
        fake_id = "00000000-0000-0000-0000-000000000000"
        
        response = api_client.get(f"{api_client.base_url}/sandboxes/{fake_id}")
        
        # Should return 404
        assert response.status_code == 404
    
    def test_create_sandbox_without_api_key(self, base_url):
        """Test that creating a sandbox without API key fails"""
        response = requests.post(
            f"{base_url}/sandboxes",
            json={
                "name": "unauthorized-sandbox",
                "image": "python:3.11"
            },
            headers={"Content-Type": "application/json"}  # No API key
        )
        
        assert response.status_code == 401
    
    def test_create_sandbox_with_all_fields(self, api_client):
        """Test creating a sandbox with all optional fields"""
        sandbox_data = {
            "name": "full-sandbox",
            "image": "python:3.11",
            "region": "default",
            "cpu": 2,
            "memory": 4,
            "disk": 20,
            "gpu": 0,
            "env": {
                "MY_VAR": "test_value",
                "ANOTHER_VAR": "another_value"
            },
            "labels": {
                "team": "engineering",
                "project": "test"
            },
            "autoStart": True
        }
        
        response = api_client.post(
            f"{api_client.base_url}/sandboxes",
            json=sandbox_data
        )
        
        # Verify response
        assert response.status_code == 201
        sandbox = response.json()
        
        # Verify all fields
        assert sandbox["name"] == sandbox_data["name"]
        assert sandbox["cpu"] == sandbox_data["cpu"]
        assert sandbox["memory"] == sandbox_data["memory"]
        assert sandbox["disk"] == sandbox_data["disk"]
        assert sandbox["env"] == sandbox_data["env"]
        assert sandbox["labels"] == sandbox_data["labels"]
        
        # Cleanup
        api_client.delete(f"{api_client.base_url}/sandboxes/{sandbox['id']}", params={"force": "true"})
    
    def test_list_sandboxes_with_pagination(self, api_client):
        """Test listing sandboxes with pagination parameters"""
        # Create multiple sandboxes
        sandbox_ids = []
        for i in range(3):
            response = api_client.post(
                f"{api_client.base_url}/sandboxes",
                json={
                    "name": f"pagination-test-{i}",
                    "image": "python:3.11"
                }
            )
            assert response.status_code == 201
            sandbox_ids.append(response.json()["id"])
        
        try:
            # Test with limit
            response = api_client.get(f"{api_client.base_url}/sandboxes", params={"limit": 2})
            assert response.status_code == 200
            data = response.json()
            assert len(data["items"]) <= 2
            assert data["limit"] == 2
            
            # Test with offset
            response = api_client.get(f"{api_client.base_url}/sandboxes", params={"offset": 1, "limit": 2})
            assert response.status_code == 200
            data = response.json()
            assert data["offset"] == 1
            
        finally:
            # Cleanup
            for sandbox_id in sandbox_ids:
                try:
                    api_client.delete(f"{api_client.base_url}/sandboxes/{sandbox_id}", params={"force": "true"})
                except Exception:
                    pass


if __name__ == "__main__":
    # Run tests
    pytest.main([__file__, "-v"])

"""
Pytest configuration for integration tests
"""
import os
import pytest
import subprocess
import time


def pytest_addoption(parser):
    """Add custom command line options"""
    parser.addoption(
        "--base-url",
        action="store",
        default="http://localhost:3000",
        help="Base URL for the API server"
    )
    parser.addoption(
        "--api-key",
        action="store",
        default="iso_sk_demo",
        help="API key for authentication"
    )
    parser.addoption(
        "--docker-compose",
        action="store_true",
        default=False,
        help="Start and stop docker-compose automatically"
    )


@pytest.fixture(scope="session")
def base_url(request):
    """Get base URL from command line or environment"""
    return os.getenv("TEST_BASE_URL", request.config.getoption("--base-url"))


@pytest.fixture(scope="session")
def api_key(request):
    """Get API key from command line or environment"""
    return os.getenv("TEST_API_KEY", request.config.getoption("--api-key"))


@pytest.fixture(scope="session", autouse=True)
def docker_compose_services(request):
    """Optionally start docker-compose services for tests"""
    if not request.config.getoption("--docker-compose"):
        yield
        return
    
    # Start docker-compose
    compose_file = "docker-compose.debug.yml"
    print(f"\nStarting services with {compose_file}...")
    subprocess.run(
        ["docker-compose", "-f", compose_file, "up", "-d"],
        check=True,
        capture_output=True
    )
    
    # Wait for services to be ready
    print("Waiting for services to be ready...")
    time.sleep(5)  # Give services time to start
        
    yield
    
    # Cleanup: stop docker-compose
    print("\nStopping services...")
    subprocess.run(
        ["docker-compose", "-f", compose_file, "down"],
        check=True,
        capture_output=True
    )

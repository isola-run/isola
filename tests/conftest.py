"""
Pytest configuration and fixtures for Isola E2E tests.
"""
from __future__ import annotations

import logging
import os
import time
import uuid
from typing import TYPE_CHECKING, Generator

import pytest
import requests

from client.isola_client import IsolaClient, IsolaError

if TYPE_CHECKING:
    from _pytest.fixtures import FixtureRequest

logger = logging.getLogger(__name__)


def pytest_addoption(parser: pytest.Parser) -> None:
    """Add custom command line options."""
    parser.addoption(
        "--skip-cleanup",
        action="store_true",
        default=False,
        help="Skip sandbox cleanup after tests (for debugging)",
    )


@pytest.fixture(scope="session")
def base_url() -> str:
    """Get base URL for API requests."""
    return os.getenv("ISOLA_BASE_URL", "http://localhost:30080")


@pytest.fixture(scope="session")
def api_key() -> str:
    """Get API key for authentication."""
    return os.getenv("ISOLA_API_KEY", "iso_sk_demo")


@pytest.fixture(scope="session")
def skip_cleanup(request: FixtureRequest) -> bool:
    """Check if cleanup should be skipped."""
    return request.config.getoption("--skip-cleanup")


@pytest.fixture(scope="session", autouse=True)
def wait_for_isola_gw(base_url: str) -> None:
    """Wait for isola-gw to be ready before running tests."""
    max_attempts = 30
    for i in range(max_attempts):
        try:
            resp = requests.get(f"{base_url}/health", timeout=5)
            if resp.ok:
                logger.info(f"isola-gw ready at {base_url}")
                return
        except requests.RequestException:
            pass

        if i < max_attempts - 1:
            logger.info(f"Waiting for isola-gw... ({i + 1}/{max_attempts})")
            time.sleep(2)

    pytest.fail(f"isola-gw not ready at {base_url} after 60s")


@pytest.fixture(scope="session")
def isola_client(base_url: str, api_key: str) -> IsolaClient:
    """Create a session-scoped Isola API client."""
    client = IsolaClient(base_url=base_url, api_key=api_key)
    logger.info(f"Connected to isola-gw at {base_url}")
    return client


def _generate_sandbox_name(test_name: str) -> str:
    """Generate a unique sandbox name from test name."""
    # Clean up test name for Kubernetes naming constraints
    clean_name = test_name[:20].replace("[", "-").replace("]", "-").replace("_", "-").lower()
    short_id = uuid.uuid4().hex[:6]
    return f"e2e-{clean_name}-{short_id}"


@pytest.fixture
def sandbox(
    isola_client: IsolaClient,
    skip_cleanup: bool,
    request: FixtureRequest,
) -> Generator[dict, None, None]:
    """
    Create a running sandbox for testing. Cleans up after test unless --skip-cleanup.

    Usage:
        def test_something(sandbox):
            sandbox_id = sandbox["id"]
            # ... test logic
    """
    sandbox_name = _generate_sandbox_name(request.node.name)

    # Create sandbox with autoStart=true
    sandbox_data = isola_client.create_sandbox(
        name=sandbox_name,
        auto_start=True,
    )
    sandbox_id = sandbox_data["id"]
    logger.info(f"Created sandbox: {sandbox_id} (name={sandbox_name})")

    # Wait for it to be running
    try:
        sandbox_data = isola_client.wait_for_ready(sandbox_id, timeout=90)
    except Exception as e:
        # If we fail to get it running, still try to clean up
        if not skip_cleanup:
            try:
                isola_client.terminate_sandbox(sandbox_id, force=True)
            except Exception:
                pass
        raise e

    yield sandbox_data

    # Cleanup
    if not skip_cleanup:
        try:
            isola_client.terminate_sandbox(sandbox_id)
            logger.info(f"Terminated sandbox: {sandbox_id}")
        except IsolaError as e:
            if e.status_code != 404:
                logger.warning(f"Failed to cleanup sandbox {sandbox_id}: {e}")


@pytest.fixture
def sandbox_stopped(
    isola_client: IsolaClient,
    skip_cleanup: bool,
    request: FixtureRequest,
) -> Generator[dict, None, None]:
    """Create a sandbox in stopped state (autoStart=false)."""
    sandbox_name = _generate_sandbox_name(request.node.name)

    sandbox_data = isola_client.create_sandbox(
        name=sandbox_name,
        auto_start=False,
    )
    sandbox_id = sandbox_data["id"]
    logger.info(f"Created stopped sandbox: {sandbox_id}")

    yield sandbox_data

    if not skip_cleanup:
        try:
            isola_client.terminate_sandbox(sandbox_id, force=True)
        except IsolaError as e:
            if e.status_code != 404:
                logger.warning(f"Failed to cleanup sandbox {sandbox_id}: {e}")


@pytest.fixture
def unique_name() -> str:
    """Generate a unique name for test resources."""
    return f"test-{uuid.uuid4().hex[:8]}"

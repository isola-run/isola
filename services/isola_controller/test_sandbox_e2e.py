"""
End-to-end test for Sandbox creation via isola_controller gateway.

This test verifies that:
1. The gateway creates a Sandbox CR
2. The isola-operator reconciles it and creates a Pod
3. The sandbox becomes running
4. The sandbox can be deleted

Prerequisites:
- minikube cluster running with isola-operator deployed
- Default SandboxTemplate applied
- isola_controller gateway accessible

Usage:
    pytest test_sandbox_e2e.py -v
    # Or with custom gateway URL:
    GATEWAY_URL=http://localhost:30080 pytest test_sandbox_e2e.py -v
"""

import os
import time
import uuid
import pytest
import requests
from concurrent.futures import ThreadPoolExecutor, as_completed
import threading


GATEWAY_URL = os.getenv("GATEWAY_URL", "http://192.168.49.2:30080")
API_KEY = os.getenv("API_KEY", "iso_sk_demo")
TIMEOUT_SECONDS = 120
POLL_INTERVAL = 2


@pytest.fixture
def api_headers():
    return {"X-API-Key": API_KEY, "Content-Type": "application/json"}


@pytest.fixture
def gateway_url():
    return GATEWAY_URL


class TestSandboxE2E:
    """End-to-end tests for Sandbox lifecycle via gateway."""

    def test_health_check(self, gateway_url, api_headers):
        """Verify the gateway is healthy before running tests."""
        response = requests.get(f"{gateway_url}/health", timeout=10)
        assert response.status_code == 200
        data = response.json()
        assert data.get("status") == "healthy"

    def test_create_sandbox_and_verify_pod(self, gateway_url, api_headers):
        """
        Test complete sandbox lifecycle:
        1. Create sandbox via gateway
        2. Poll until running
        3. Verify pod was created by operator
        4. Delete sandbox
        """
        sandbox_name = f"e2e-test-{uuid.uuid4().hex[:8]}"
        sandbox_id = None

        try:
            # Step 1: Create sandbox
            print(f"\n[1/4] Creating sandbox '{sandbox_name}'...")
            create_response = requests.post(
                f"{gateway_url}/sandboxes",
                headers=api_headers,
                json={
                    "name": sandbox_name,
                    "autoStart": True,
                },
                timeout=30,
            )
            assert create_response.status_code == 201, (
                f"Expected 201, got {create_response.status_code}: {create_response.text}"
            )
            
            sandbox_data = create_response.json()
            sandbox_id = sandbox_data.get("id")
            assert sandbox_id, "Sandbox ID not returned"
            print(f"    ✓ Sandbox created with ID: {sandbox_id}")

            # Step 2: Poll until running
            print(f"[2/4] Waiting for sandbox to become running (timeout={TIMEOUT_SECONDS}s)...")
            start_time = time.time()
            last_state = None
            
            while time.time() - start_time < TIMEOUT_SECONDS:
                get_response = requests.get(
                    f"{gateway_url}/sandboxes/{sandbox_id}",
                    headers=api_headers,
                    timeout=10,
                )
                
                if get_response.status_code == 404:
                    print(f"    Sandbox not found yet, retrying...")
                    time.sleep(POLL_INTERVAL)
                    continue
                
                assert get_response.status_code == 200, (
                    f"Expected 200, got {get_response.status_code}: {get_response.text}"
                )
                
                sandbox_status = get_response.json()
                state = sandbox_status.get("state")
                
                if state != last_state:
                    print(f"    State: {state}")
                    last_state = state
                
                if state == "running":
                    print(f"    ✓ Sandbox is running!")
                    break
                elif state == "error":
                    error_reason = sandbox_status.get("errorReason")
                    pytest.fail(f"Sandbox entered error state: {error_reason}")
                
                time.sleep(POLL_INTERVAL)
            else:
                pytest.fail(
                    f"Timeout waiting for sandbox to become running. Last state: {last_state}"
                )

            # Step 3: Verify the sandbox has expected properties
            print(f"[3/4] Verifying sandbox properties...")
            final_response = requests.get(
                f"{gateway_url}/sandboxes/{sandbox_id}",
                headers=api_headers,
                timeout=10,
            )
            assert final_response.status_code == 200
            final_data = final_response.json()
            
            assert final_data.get("state") == "running"
            assert final_data.get("name") == sandbox_name
            print(f"    ✓ Sandbox properties verified")

            # # Step 4: Delete sandbox
            # print(f"[4/4] Deleting sandbox...")
            # delete_response = requests.delete(
            #     f"{gateway_url}/sandboxes/{sandbox_id}",
            #     headers=api_headers,
            #     timeout=30,
            # )
            # assert delete_response.status_code == 204, (
            #     f"Expected 204, got {delete_response.status_code}: {delete_response.text}"
            # )
            # print(f"    ✓ Sandbox deleted successfully")

            # # Verify it's gone
            # time.sleep(2)
            # verify_response = requests.get(
            #     f"{gateway_url}/sandboxes/{sandbox_id}",
            #     headers=api_headers,
            #     timeout=10,
            # )
            # assert verify_response.status_code == 404, "Sandbox should no longer exist"
            # print(f"    ✓ Verified sandbox no longer exists")

        except Exception as e:
            # Cleanup on failure
            if sandbox_id:
                print(f"\n[Cleanup] Attempting to delete sandbox {sandbox_id}...")
                try:
                    requests.delete(
                        f"{gateway_url}/sandboxes/{sandbox_id}",
                        headers=api_headers,
                        timeout=10,
                    )
                except Exception:
                    pass
            raise

    def test_create_10_sandboxes_in_parallel(self, gateway_url, api_headers):
        """
        Test running the full lifecycle for 10 sandboxes in parallel.
        This includes creation, polling, verification, and deletion for each.
        """
        num_sandboxes = 999

        def run_full_sandbox_lifecycle(sandbox_name: str) -> str:
            """
            Performs a full sandbox lifecycle and returns the sandbox_name on success.
            """
            sandbox_id = None
            try:
                # Step 1: Create sandbox
                print(f"\n[1/5] Creating sandbox '{sandbox_name}'...")
                create_response = requests.post(
                    f"{gateway_url}/sandboxes",
                    headers=api_headers,
                    json={"name": sandbox_name, "autoStart": True},
                    timeout=30,
                )
                assert create_response.status_code == 201, (
                    f"Expected 201, got {create_response.status_code}: {create_response.text}"
                )

                sandbox_data = create_response.json()
                sandbox_id = sandbox_data.get("id")
                assert sandbox_id, "Sandbox ID not returned"
                print(f"    ✓ [{sandbox_name}] Created with ID: {sandbox_id}")

                # Step 2: Poll until running
                print(f"[2/5] [{sandbox_name}] Waiting for sandbox to become running (timeout={TIMEOUT_SECONDS}s)...")
                start_time = time.time()
                last_state = None

                while time.time() - start_time < TIMEOUT_SECONDS:
                    get_response = requests.get(
                        f"{gateway_url}/sandboxes/{sandbox_id}",
                        headers=api_headers,
                        timeout=10,
                    )

                    if get_response.status_code == 404:
                        print(f"    [{sandbox_name}] Sandbox not found yet, retrying...")
                        time.sleep(POLL_INTERVAL)
                        continue

                    assert get_response.status_code == 200, (
                        f"Expected 200, got {get_response.status_code}: {get_response.text}"
                    )

                    sandbox_status = get_response.json()
                    state = sandbox_status.get("state")

                    if state != last_state:
                        print(f"    [{sandbox_name}] State: {state}")
                        last_state = state

                    if state == "running":
                        print(f"    ✓ [{sandbox_name}] Sandbox is running!")
                        break
                    elif state == "error":
                        error_reason = sandbox_status.get("errorReason")
                        pytest.fail(f"Sandbox {sandbox_name} entered error state: {error_reason}")

                    time.sleep(POLL_INTERVAL)
                else:
                    pytest.fail(
                        f"Timeout waiting for sandbox to become running. Last state: {last_state}"
                    )

                # Step 3: Verify the sandbox has expected properties
                print(f"[3/5] [{sandbox_name}] Verifying sandbox properties...")
                final_response = requests.get(
                    f"{gateway_url}/sandboxes/{sandbox_id}",
                    headers=api_headers,
                    timeout=10,
                )
                assert final_response.status_code == 200
                final_data = final_response.json()

                assert final_data.get("state") == "running"
                assert final_data.get("name") == sandbox_name
                print(f"    ✓ [{sandbox_name}] Sandbox properties verified")

                # Step 4: Delete sandbox
                #time.sleep(60)
                print(f"[4/5] [{sandbox_name}] Deleting sandbox...")
                delete_response = requests.delete(
                    f"{gateway_url}/sandboxes/{sandbox_id}",
                    headers=api_headers,
                    timeout=30,
                )
                assert delete_response.status_code == 204, (
                    f"Expected 204, got {delete_response.status_code}: {delete_response.text}"
                )
                print(f"    ✓ [{sandbox_name}] Sandbox deleted successfully")

                # Step 5: Verify it's gone
                print(f"[5/5] [{sandbox_name}] Verifying deletion...")
                time.sleep(2)
                verify_response = requests.get(
                    f"{gateway_url}/sandboxes/{sandbox_id}",
                    headers=api_headers,
                    timeout=10,
                )
                assert verify_response.status_code == 404, "Sandbox should no longer exist"
                print(f"    ✓ [{sandbox_name}] Verified sandbox no longer exists")

                return sandbox_name

            except Exception as e:
                # On failure, try to clean up if we have a sandbox_id
                if sandbox_id:
                    print(f"\n[Cleanup] Attempting to delete failed sandbox {sandbox_id}...")
                    try:
                        requests.delete(
                            f"{gateway_url}/sandboxes/{sandbox_id}",
                            headers=api_headers,
                            timeout=10,
                        )
                    except Exception:
                        pass  # Don't obscure original error
                raise e

        sandbox_names = [f"e2e-parallel-{i}-{uuid.uuid4().hex[:6]}" for i in range(num_sandboxes)]

        with ThreadPoolExecutor(max_workers=num_sandboxes) as executor:
            print(f"\n🚀 Launching {num_sandboxes} full sandbox lifecycles in parallel...")
            future_to_name = {executor.submit(run_full_sandbox_lifecycle, name): name for name in sandbox_names}

            success_count = 0
            for future in as_completed(future_to_name):
                name = future_to_name[future]
                try:
                    result = future.result()
                    print(f"✅ SUCCESS: Full lifecycle completed for sandbox '{result}'.")
                    success_count += 1
                except Exception as exc:
                    pytest.fail(f"🚨 FAILED: Lifecycle for sandbox '{name}' failed: {exc}")

        assert success_count == num_sandboxes, f"Expected {num_sandboxes} lifecycles to succeed, but only {success_count} did."
        print(f"\n✅ All {num_sandboxes} sandbox lifecycles completed successfully.")

    def test_list_sandboxes(self, gateway_url, api_headers):
        """Test listing sandboxes endpoint."""
        response = requests.get(
            f"{gateway_url}/sandboxes",
            headers=api_headers,
            timeout=10,
        )
        assert response.status_code == 200
        data = response.json()
        assert "items" in data
        assert "total" in data
        print(f"\n✓ Listed {len(data['items'])} sandboxes (total: {data['total']})")


if __name__ == "__main__":
    pytest.main([__file__, "-v", "-s"])

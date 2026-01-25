"""
Stress tests for sandbox churn scenarios.

These tests are marked with @pytest.mark.stress and are not run by default.
Run with: pytest -m stress tests/e2e/test_stress.py

For verbose output: pytest -m stress -v --tb=short tests/e2e/test_stress.py
"""
from __future__ import annotations

import logging
import statistics
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass, field
from typing import TYPE_CHECKING

import pytest

if TYPE_CHECKING:
    from client.isola_client import IsolaClient

logger = logging.getLogger(__name__)


@dataclass
class BenchmarkResult:
    """Result of a single benchmark operation."""

    operation: str
    duration_ms: float
    success: bool
    error: str | None = None


@dataclass
class BenchmarkSummary:
    """Summary of benchmark results."""

    results: list[BenchmarkResult] = field(default_factory=list)

    def add(self, result: BenchmarkResult) -> None:
        self.results.append(result)

    def report(self) -> None:
        """Print a summary of benchmark results."""
        successful = [r for r in self.results if r.success]
        failed = [r for r in self.results if not r.success]

        print(f"\n{'=' * 60}")
        print("BENCHMARK RESULTS")
        print(f"{'=' * 60}")
        print(f"Total operations: {len(self.results)}")
        print(f"Successful: {len(successful)}")
        print(f"Failed: {len(failed)}")

        for op in ["create", "ready", "delete"]:
            op_results = [r.duration_ms for r in successful if r.operation == op]
            if not op_results:
                continue

            sorted_results = sorted(op_results)
            n = len(sorted_results)

            print(f"\n{op.upper()} latency (ms):")
            print(f"  Count: {n}")
            print(f"  Min: {min(sorted_results):.2f}")
            print(f"  P50: {sorted_results[n // 2]:.2f}")
            print(f"  P95: {sorted_results[int(n * 0.95)]:.2f}")
            print(f"  P99: {sorted_results[int(n * 0.99)]:.2f}")
            print(f"  Max: {max(sorted_results):.2f}")
            print(f"  Avg: {statistics.mean(sorted_results):.2f}")
            if n > 1:
                print(f"  Std: {statistics.stdev(sorted_results):.2f}")

        if failed:
            print(f"\nFailure breakdown:")
            errors: dict[str, int] = {}
            for r in failed:
                key = f"{r.operation}: {r.error or 'unknown'}"
                errors[key] = errors.get(key, 0) + 1
            for error, count in sorted(errors.items(), key=lambda x: -x[1]):
                print(f"  {error}: {count}")


@pytest.fixture
def benchmark_summary() -> BenchmarkSummary:
    """Fixture to collect and report benchmark metrics."""
    summary = BenchmarkSummary()
    yield summary
    summary.report()


@pytest.mark.stress
class TestSandboxChurn:
    """High-frequency sandbox creation/deletion tests."""

    @pytest.mark.timeout(600)  # 10 minutes
    def test_concurrent_sandbox_creation(
        self,
        isola_client: IsolaClient,
        benchmark_summary: BenchmarkSummary,
        unique_name: str,
        skip_cleanup: bool,
    ) -> None:
        """Create N sandboxes concurrently and measure latency."""
        NUM_SANDBOXES = 20
        MAX_WORKERS = 10
        sandbox_ids: list[str] = []

        def create_sandbox(i: int) -> tuple[BenchmarkResult, str | None]:
            name = f"{unique_name}-{i}"
            start = time.perf_counter()
            try:
                resp = isola_client.create_sandbox(
                    name=name,
                    auto_start=True,
                )
                duration = (time.perf_counter() - start) * 1000
                return (
                    BenchmarkResult("create", duration, True),
                    resp["id"],
                )
            except Exception as e:
                duration = (time.perf_counter() - start) * 1000
                return (
                    BenchmarkResult("create", duration, False, str(e)),
                    None,
                )

        logger.info(f"Creating {NUM_SANDBOXES} sandboxes concurrently...")

        # Concurrent creation
        with ThreadPoolExecutor(max_workers=MAX_WORKERS) as executor:
            futures = [executor.submit(create_sandbox, i) for i in range(NUM_SANDBOXES)]
            for future in as_completed(futures):
                result, sandbox_id = future.result()
                benchmark_summary.add(result)
                if sandbox_id:
                    sandbox_ids.append(sandbox_id)

        logger.info(f"Created {len(sandbox_ids)}/{NUM_SANDBOXES} sandboxes")

        # Wait for all to be ready
        def wait_for_ready(sandbox_id: str) -> BenchmarkResult:
            start = time.perf_counter()
            try:
                isola_client.wait_for_ready(sandbox_id, timeout=120)
                duration = (time.perf_counter() - start) * 1000
                return BenchmarkResult("ready", duration, True)
            except Exception as e:
                duration = (time.perf_counter() - start) * 1000
                return BenchmarkResult("ready", duration, False, str(e))

        logger.info("Waiting for sandboxes to be ready...")
        with ThreadPoolExecutor(max_workers=MAX_WORKERS) as executor:
            futures = [executor.submit(wait_for_ready, sid) for sid in sandbox_ids]
            for future in as_completed(futures):
                benchmark_summary.add(future.result())

        # Cleanup
        if not skip_cleanup:

            def delete_sandbox(sandbox_id: str) -> BenchmarkResult:
                start = time.perf_counter()
                try:
                    isola_client.terminate_sandbox(sandbox_id)
                    duration = (time.perf_counter() - start) * 1000
                    return BenchmarkResult("delete", duration, True)
                except Exception as e:
                    duration = (time.perf_counter() - start) * 1000
                    return BenchmarkResult("delete", duration, False, str(e))

            logger.info("Cleaning up sandboxes...")
            with ThreadPoolExecutor(max_workers=MAX_WORKERS) as executor:
                futures = [executor.submit(delete_sandbox, sid) for sid in sandbox_ids]
                for future in as_completed(futures):
                    benchmark_summary.add(future.result())

    @pytest.mark.timeout(1800)  # 30 minutes
    def test_sustained_churn(
        self,
        isola_client: IsolaClient,
        benchmark_summary: BenchmarkSummary,
        unique_name: str,
        skip_cleanup: bool,
    ) -> None:
        """Sustained create/delete cycles for extended period."""
        DURATION_SECONDS = 300  # 5 minutes
        TARGET_CONCURRENT = 10
        SANDBOX_LIFETIME_SECONDS = 30

        active_sandboxes: dict[str, float] = {}  # id -> creation time
        start_time = time.time()
        iteration = 0

        logger.info(f"Starting sustained churn test for {DURATION_SECONDS}s")

        while time.time() - start_time < DURATION_SECONDS:
            # Create sandboxes up to target
            while len(active_sandboxes) < TARGET_CONCURRENT:
                sandbox_name = f"{unique_name}-{iteration}"
                start = time.perf_counter()
                try:
                    resp = isola_client.create_sandbox(
                        name=sandbox_name,
                        auto_start=True,
                    )
                    duration = (time.perf_counter() - start) * 1000
                    active_sandboxes[resp["id"]] = time.time()
                    benchmark_summary.add(BenchmarkResult("create", duration, True))
                except Exception as e:
                    duration = (time.perf_counter() - start) * 1000
                    benchmark_summary.add(BenchmarkResult("create", duration, False, str(e)))
                iteration += 1

            # Delete sandboxes that have been running long enough
            now = time.time()
            to_delete = [
                sid
                for sid, created in active_sandboxes.items()
                if now - created > SANDBOX_LIFETIME_SECONDS
            ]

            for sandbox_id in to_delete:
                start = time.perf_counter()
                try:
                    isola_client.terminate_sandbox(sandbox_id)
                    del active_sandboxes[sandbox_id]
                    duration = (time.perf_counter() - start) * 1000
                    benchmark_summary.add(BenchmarkResult("delete", duration, True))
                except Exception as e:
                    duration = (time.perf_counter() - start) * 1000
                    benchmark_summary.add(BenchmarkResult("delete", duration, False, str(e)))
                    # Remove from tracking even if delete failed
                    active_sandboxes.pop(sandbox_id, None)

            time.sleep(1)

        # Cleanup remaining sandboxes
        if not skip_cleanup:
            logger.info(f"Cleaning up {len(active_sandboxes)} remaining sandboxes...")
            for sandbox_id in list(active_sandboxes.keys()):
                try:
                    isola_client.terminate_sandbox(sandbox_id)
                except Exception:
                    pass

        logger.info(f"Completed {iteration} iterations")

    @pytest.mark.timeout(300)  # 5 minutes
    def test_rapid_create_delete(
        self,
        isola_client: IsolaClient,
        benchmark_summary: BenchmarkSummary,
        unique_name: str,
    ) -> None:
        """Rapidly create and immediately delete sandboxes."""
        NUM_OPERATIONS = 50

        logger.info(f"Running {NUM_OPERATIONS} rapid create/delete cycles...")

        for i in range(NUM_OPERATIONS):
            sandbox_name = f"{unique_name}-rapid-{i}"

            # Create
            create_start = time.perf_counter()
            try:
                resp = isola_client.create_sandbox(
                    name=sandbox_name,
                    auto_start=True,
                )
                create_duration = (time.perf_counter() - create_start) * 1000
                benchmark_summary.add(BenchmarkResult("create", create_duration, True))
                sandbox_id = resp["id"]
            except Exception as e:
                create_duration = (time.perf_counter() - create_start) * 1000
                benchmark_summary.add(BenchmarkResult("create", create_duration, False, str(e)))
                continue

            # Brief pause
            time.sleep(0.5)

            # Delete immediately (don't wait for ready)
            delete_start = time.perf_counter()
            try:
                isola_client.terminate_sandbox(sandbox_id, force=True)
                delete_duration = (time.perf_counter() - delete_start) * 1000
                benchmark_summary.add(BenchmarkResult("delete", delete_duration, True))
            except Exception as e:
                delete_duration = (time.perf_counter() - delete_start) * 1000
                benchmark_summary.add(BenchmarkResult("delete", delete_duration, False, str(e)))


@pytest.mark.stress
class TestResourceExhaustion:
    """Tests for resource limits and exhaustion scenarios."""

    @pytest.mark.timeout(600)
    def test_max_concurrent_sandboxes(
        self,
        isola_client: IsolaClient,
        benchmark_summary: BenchmarkSummary,
        unique_name: str,
        skip_cleanup: bool,
    ) -> None:
        """Try to create as many sandboxes as possible until hitting limits."""
        MAX_SANDBOXES = 100
        sandbox_ids: list[str] = []
        failures_in_a_row = 0
        MAX_CONSECUTIVE_FAILURES = 5

        logger.info(f"Attempting to create up to {MAX_SANDBOXES} sandboxes...")

        for i in range(MAX_SANDBOXES):
            sandbox_name = f"{unique_name}-max-{i}"
            start = time.perf_counter()

            try:
                resp = isola_client.create_sandbox(
                    name=sandbox_name,
                    auto_start=True,
                )
                duration = (time.perf_counter() - start) * 1000
                benchmark_summary.add(BenchmarkResult("create", duration, True))
                sandbox_ids.append(resp["id"])
                failures_in_a_row = 0
                logger.info(f"Created sandbox {i + 1}/{MAX_SANDBOXES}")
            except Exception as e:
                duration = (time.perf_counter() - start) * 1000
                benchmark_summary.add(BenchmarkResult("create", duration, False, str(e)))
                failures_in_a_row += 1
                logger.warning(f"Failed to create sandbox {i + 1}: {e}")

                if failures_in_a_row >= MAX_CONSECUTIVE_FAILURES:
                    logger.info(f"Stopping after {MAX_CONSECUTIVE_FAILURES} consecutive failures")
                    break

            # Small delay between creations
            time.sleep(0.2)

        logger.info(f"Successfully created {len(sandbox_ids)} sandboxes")

        # Cleanup
        if not skip_cleanup:
            logger.info("Cleaning up...")
            for sandbox_id in sandbox_ids:
                try:
                    isola_client.terminate_sandbox(sandbox_id, force=True)
                except Exception:
                    pass

#!/usr/bin/env python3
"""
Benchmark baseline comparison tool.

Compares current benchmark results against a baseline and detects regressions.

Usage:
    python compare_baseline.py --current results.json --baseline baseline.json
    python compare_baseline.py --current results.json --threshold 0.20
"""

import argparse
import json
import sys
from pathlib import Path
from dataclasses import dataclass
from typing import Any


@dataclass
class Regression:
    """Represents a detected regression."""
    metric: str
    percentile: str
    baseline: float
    current: float
    change_pct: float


def load_k6_results(path: str) -> dict[str, dict[str, float]]:
    """Parse k6 JSON output into metric summaries."""
    metrics: dict[str, list[float]] = {}

    with open(path) as f:
        for line in f:
            try:
                data = json.loads(line)
            except json.JSONDecodeError:
                continue

            if data.get("type") == "Point":
                metric = data.get("metric", "")
                value = data.get("data", {}).get("value")
                if metric and value is not None:
                    if metric not in metrics:
                        metrics[metric] = []
                    metrics[metric].append(value)

    # Calculate percentiles
    results: dict[str, dict[str, float]] = {}
    for metric, values in metrics.items():
        if not values:
            continue
        values.sort()
        n = len(values)
        results[metric] = {
            "min": values[0],
            "p50": values[n // 2],
            "p95": values[int(n * 0.95)] if n > 1 else values[0],
            "p99": values[int(n * 0.99)] if n > 1 else values[0],
            "max": values[-1],
            "count": n,
            "avg": sum(values) / n,
        }

    return results


def compare_metrics(
    current: dict[str, dict[str, float]],
    baseline: dict[str, dict[str, float]],
    threshold: float,
) -> list[Regression]:
    """Compare current results against baseline and find regressions."""
    regressions: list[Regression] = []

    # Key metrics to compare (higher is worse)
    latency_metrics = [
        "sandbox_create_duration_ms",
        "sandbox_ready_duration_ms",
        "sandbox_delete_duration_ms",
        "http_req_duration",
        "sustained_create_duration_ms",
        "sustained_ready_duration_ms",
        "burst_create_duration_ms",
    ]

    for metric in latency_metrics:
        if metric not in current or metric not in baseline:
            continue

        current_vals = current[metric]
        baseline_vals = baseline[metric]

        for percentile in ["p50", "p95", "p99"]:
            curr = current_vals.get(percentile, 0)
            base = baseline_vals.get(percentile, 0)

            if base <= 0:
                continue

            change = (curr - base) / base
            if change > threshold:
                regressions.append(Regression(
                    metric=metric,
                    percentile=percentile,
                    baseline=base,
                    current=curr,
                    change_pct=change * 100,
                ))

    return regressions


def print_comparison(
    current: dict[str, dict[str, float]],
    baseline: dict[str, dict[str, float]],
) -> None:
    """Print a comparison table of current vs baseline metrics."""
    print("\n" + "=" * 80)
    print("METRIC COMPARISON")
    print("=" * 80)
    print(f"{'Metric':<40} {'Baseline P95':>15} {'Current P95':>15} {'Change':>10}")
    print("-" * 80)

    all_metrics = sorted(set(current.keys()) | set(baseline.keys()))
    for metric in all_metrics:
        curr = current.get(metric, {}).get("p95", 0)
        base = baseline.get(metric, {}).get("p95", 0)

        if base > 0:
            change = ((curr - base) / base) * 100
            change_str = f"{change:+.1f}%"
        else:
            change_str = "N/A"

        print(f"{metric:<40} {base:>15.2f} {curr:>15.2f} {change_str:>10}")


def main() -> int:
    parser = argparse.ArgumentParser(description="Compare benchmark results against baseline")
    parser.add_argument("--current", required=True, help="Path to current results (k6 JSON)")
    parser.add_argument("--baseline", default="tests/benchmark/baseline.json",
                        help="Path to baseline results")
    parser.add_argument("--threshold", type=float, default=0.20,
                        help="Regression threshold (0.20 = 20%% increase)")
    parser.add_argument("--save-baseline", action="store_true",
                        help="Save current results as new baseline")
    args = parser.parse_args()

    # Load current results
    print(f"Loading current results from {args.current}...")
    try:
        current = load_k6_results(args.current)
    except FileNotFoundError:
        print(f"ERROR: Current results file not found: {args.current}")
        return 1
    except Exception as e:
        print(f"ERROR: Failed to parse current results: {e}")
        return 1

    if not current:
        print("ERROR: No metrics found in current results")
        return 1

    print(f"Loaded {len(current)} metrics from current run")

    # Check if baseline exists
    baseline_path = Path(args.baseline)
    if not baseline_path.exists():
        if args.save_baseline:
            print(f"No baseline found. Saving current results as baseline to {args.baseline}")
            baseline_path.parent.mkdir(parents=True, exist_ok=True)
            with open(baseline_path, "w") as f:
                json.dump(current, f, indent=2)
            print("Baseline saved successfully")
            return 0
        else:
            print(f"No baseline found at {args.baseline}")
            print("Run with --save-baseline to create a new baseline")
            return 0

    # Load baseline
    print(f"Loading baseline from {args.baseline}...")
    with open(baseline_path) as f:
        baseline = json.load(f)

    print(f"Loaded {len(baseline)} metrics from baseline")

    # Print comparison table
    print_comparison(current, baseline)

    # Find regressions
    regressions = compare_metrics(current, baseline, args.threshold)

    if regressions:
        print("\n" + "=" * 80)
        print(f"PERFORMANCE REGRESSIONS DETECTED (threshold: {args.threshold * 100:.0f}%)")
        print("=" * 80)
        for r in sorted(regressions, key=lambda x: -x.change_pct):
            print(f"  {r.metric} {r.percentile}: "
                  f"{r.baseline:.2f} → {r.current:.2f} "
                  f"(+{r.change_pct:.1f}%)")
        print()
        return 1

    print("\n" + "=" * 80)
    print("NO SIGNIFICANT REGRESSIONS DETECTED")
    print("=" * 80)

    # Optionally update baseline
    if args.save_baseline:
        print(f"Updating baseline at {args.baseline}...")
        with open(baseline_path, "w") as f:
            json.dump(current, f, indent=2)
        print("Baseline updated")

    return 0


if __name__ == "__main__":
    sys.exit(main())

#!/bin/bash
#
# Resource monitoring script for Isola benchmark runs.
#
# Usage:
#   ./monitor.sh [OPTIONS]
#
# Options:
#   -d, --duration    Duration in seconds (default: 300)
#   -o, --output-dir  Output directory (default: ./benchmark-results)
#   -n, --namespace   Kubernetes namespace (default: isola-system)
#   -i, --interval    Polling interval in seconds (default: 5)
#
# Example:
#   ./monitor.sh -d 600 -o ./results -n isola-system

set -euo pipefail

# Default values
DURATION=300
OUTPUT_DIR="./benchmark-results"
NAMESPACE="isola-system"
INTERVAL=5

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -d|--duration)
            DURATION="$2"
            shift 2
            ;;
        -o|--output-dir)
            OUTPUT_DIR="$2"
            shift 2
            ;;
        -n|--namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        -i|--interval)
            INTERVAL="$2"
            shift 2
            ;;
        -h|--help)
            grep '^#' "$0" | grep -v '#!/' | sed 's/^# *//'
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Create output directory
mkdir -p "$OUTPUT_DIR"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

echo "Starting resource monitoring..."
echo "  Duration: ${DURATION}s"
echo "  Interval: ${INTERVAL}s"
echo "  Namespace: ${NAMESPACE}"
echo "  Output: ${OUTPUT_DIR}"

# Start background collectors

# 1. Pod resource usage over time
(
    echo "timestamp,pod,container,cpu,memory" > "${OUTPUT_DIR}/pod_resources_${TIMESTAMP}.csv"
    END_TIME=$(($(date +%s) + DURATION))
    while [[ $(date +%s) -lt $END_TIME ]]; do
        TS=$(date +%s)
        kubectl top pods -n "$NAMESPACE" --containers 2>/dev/null | tail -n +2 | while read pod container cpu memory; do
            echo "${TS},${pod},${container},${cpu},${memory}"
        done >> "${OUTPUT_DIR}/pod_resources_${TIMESTAMP}.csv"
        sleep "$INTERVAL"
    done
) &
POD_RESOURCES_PID=$!

# 2. Sandbox resource usage
(
    echo "timestamp,pod,container,cpu,memory" > "${OUTPUT_DIR}/sandbox_resources_${TIMESTAMP}.csv"
    END_TIME=$(($(date +%s) + DURATION))
    while [[ $(date +%s) -lt $END_TIME ]]; do
        TS=$(date +%s)
        kubectl top pods -n isola-sandboxes --containers 2>/dev/null | tail -n +2 | while read pod container cpu memory; do
            echo "${TS},${pod},${container},${cpu},${memory}"
        done >> "${OUTPUT_DIR}/sandbox_resources_${TIMESTAMP}.csv"
        sleep "$INTERVAL"
    done
) &
SANDBOX_RESOURCES_PID=$!

# 3. Event monitoring
(
    kubectl get events -n isola-sandboxes -w --output-watch-events 2>/dev/null \
        > "${OUTPUT_DIR}/events_${TIMESTAMP}.log" &
    EVENT_PID=$!
    sleep "$DURATION"
    kill $EVENT_PID 2>/dev/null || true
) &
EVENTS_PID=$!

# 4. Sandbox count over time
(
    echo "timestamp,total,running,pending,error" > "${OUTPUT_DIR}/sandbox_counts_${TIMESTAMP}.csv"
    END_TIME=$(($(date +%s) + DURATION))
    while [[ $(date +%s) -lt $END_TIME ]]; do
        TS=$(date +%s)
        TOTAL=$(kubectl get sandboxes -n isola-sandboxes --no-headers 2>/dev/null | wc -l || echo 0)
        RUNNING=$(kubectl get sandboxes -n isola-sandboxes --no-headers 2>/dev/null | grep -c "Running" || echo 0)
        PENDING=$(kubectl get sandboxes -n isola-sandboxes --no-headers 2>/dev/null | grep -c "Pending" || echo 0)
        ERROR=$(kubectl get sandboxes -n isola-sandboxes --no-headers 2>/dev/null | grep -c "Error" || echo 0)
        echo "${TS},${TOTAL},${RUNNING},${PENDING},${ERROR}"
        sleep "$INTERVAL"
    done >> "${OUTPUT_DIR}/sandbox_counts_${TIMESTAMP}.csv"
) &
COUNTS_PID=$!

# Collect initial metrics
echo "Collecting initial metrics..."
kubectl get --raw /metrics 2>/dev/null | grep -E "apiserver_request_duration|etcd_request_duration" \
    > "${OUTPUT_DIR}/apiserver_metrics_start_${TIMESTAMP}.txt" || true

# Wait for monitoring duration
echo "Monitoring for ${DURATION}s..."
sleep "$DURATION"

# Collect final metrics
echo "Collecting final metrics..."
kubectl get --raw /metrics 2>/dev/null | grep -E "apiserver_request_duration|etcd_request_duration" \
    > "${OUTPUT_DIR}/apiserver_metrics_end_${TIMESTAMP}.txt" || true

# Cleanup background processes
echo "Stopping monitors..."
kill $POD_RESOURCES_PID $SANDBOX_RESOURCES_PID $EVENTS_PID $COUNTS_PID 2>/dev/null || true
wait

# Collect operator logs
echo "Collecting operator logs..."
kubectl logs -n "$NAMESPACE" -l app=isola-operator --tail=5000 \
    > "${OUTPUT_DIR}/operator_logs_${TIMESTAMP}.log" 2>/dev/null || true

# Collect gateway logs
kubectl logs -n "$NAMESPACE" -l app.kubernetes.io/name=isola-gw --tail=5000 \
    > "${OUTPUT_DIR}/gateway_logs_${TIMESTAMP}.log" 2>/dev/null || true

echo "Monitoring complete. Results saved to ${OUTPUT_DIR}"
ls -la "${OUTPUT_DIR}"/*"${TIMESTAMP}"*

/**
 * Sandbox Churn Load Test
 *
 * Tests high sandbox creation/deletion throughput to find bottlenecks
 * and establish performance baselines.
 *
 * Usage:
 *   k6 run --out json=results.json sandbox_churn.js
 *
 * Environment variables:
 *   ISOLA_API_URL: Base URL for isola-gw (default: http://localhost:8080)
 *   ISOLA_API_KEY: API key for authentication (default: iso_sk_demo)
 */

import http from 'k6/http';
import { check, sleep, fail } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

// Custom metrics for detailed analysis
const sandboxCreateDuration = new Trend('sandbox_create_duration_ms');
const sandboxReadyDuration = new Trend('sandbox_ready_duration_ms');
const sandboxDeleteDuration = new Trend('sandbox_delete_duration_ms');
const sandboxCreateErrors = new Counter('sandbox_create_errors');
const sandboxReadyErrors = new Counter('sandbox_ready_errors');
const sandboxDeleteErrors = new Counter('sandbox_delete_errors');
const failureRate = new Rate('failures');

// Test configuration with multiple scenarios
export const options = {
    scenarios: {
        // Scenario 1: Gradual ramp-up to find breaking point
        stress_test: {
            executor: 'ramping-vus',
            startVUs: 1,
            stages: [
                { duration: '1m', target: 5 },    // Warm up
                { duration: '2m', target: 20 },   // Moderate load
                { duration: '2m', target: 50 },   // High load
                { duration: '2m', target: 100 },  // Stress load
                { duration: '1m', target: 0 },    // Cool down
            ],
            gracefulRampDown: '30s',
        },
    },
    thresholds: {
        // SLO thresholds
        'sandbox_create_duration_ms': ['p(95)<5000'],   // 95th percentile < 5s
        'sandbox_ready_duration_ms': ['p(95)<60000'],   // 95th percentile < 60s
        'sandbox_delete_duration_ms': ['p(95)<5000'],   // 95th percentile < 5s
        'failures': ['rate<0.05'],                       // < 5% failure rate
        'http_req_duration': ['p(99)<10000'],           // 99th percentile < 10s
    },
};

// Configuration from environment
const BASE_URL = __ENV.ISOLA_API_URL || 'http://localhost:8080';
const API_KEY = __ENV.ISOLA_API_KEY || 'iso_sk_demo';

const headers = {
    'X-API-Key': API_KEY,
    'Content-Type': 'application/json',
};

/**
 * Wait for sandbox to reach running state
 */
function waitForReady(sandboxId, timeoutMs = 60000) {
    const startTime = Date.now();
    const pollInterval = 1000;

    while (Date.now() - startTime < timeoutMs) {
        const res = http.get(`${BASE_URL}/api/v1/sandboxes/${sandboxId}`, { headers });

        if (res.status !== 200) {
            sleep(pollInterval / 1000);
            continue;
        }

        const state = res.json('state');
        if (state === 'running') {
            return { success: true, duration: Date.now() - startTime };
        }
        if (state === 'error' || state === 'failed') {
            return { success: false, duration: Date.now() - startTime, error: `Sandbox entered ${state} state` };
        }

        sleep(pollInterval / 1000);
    }

    return { success: false, duration: Date.now() - startTime, error: 'Timeout waiting for ready state' };
}

/**
 * Main test function - creates and deletes a sandbox
 */
export default function() {
    const sandboxName = `k6-${__VU}-${__ITER}-${Date.now()}`;
    let sandboxId = null;
    let overallSuccess = true;

    // Step 1: Create sandbox
    const createStart = Date.now();
    const createPayload = JSON.stringify({
        name: sandboxName,
        autoStart: true,
        cpu: 0.1,
        memory: 0.128,
    });

    const createRes = http.post(`${BASE_URL}/api/v1/sandboxes`, createPayload, { headers });
    const createDuration = Date.now() - createStart;
    sandboxCreateDuration.add(createDuration);

    const createSuccess = check(createRes, {
        'create: status is 201': (r) => r.status === 201,
        'create: has id': (r) => r.json('id') !== undefined,
    });

    if (!createSuccess) {
        sandboxCreateErrors.add(1);
        failureRate.add(1);
        console.log(`Create failed: ${createRes.status} - ${createRes.body}`);
        return;
    }

    sandboxId = createRes.json('id');

    // Step 2: Wait for sandbox to be ready
    const readyResult = waitForReady(sandboxId, 90000);
    sandboxReadyDuration.add(readyResult.duration);

    if (!readyResult.success) {
        sandboxReadyErrors.add(1);
        overallSuccess = false;
        console.log(`Ready failed for ${sandboxId}: ${readyResult.error}`);
    }

    // Let the sandbox run briefly to simulate real workload
    sleep(Math.random() * 3 + 1);  // 1-4 seconds

    // Step 3: Delete sandbox
    const deleteStart = Date.now();
    const deleteRes = http.del(`${BASE_URL}/api/v1/sandboxes/${sandboxId}`, null, { headers });
    const deleteDuration = Date.now() - deleteStart;
    sandboxDeleteDuration.add(deleteDuration);

    const deleteSuccess = check(deleteRes, {
        'delete: status is 204': (r) => r.status === 204,
    });

    if (!deleteSuccess) {
        sandboxDeleteErrors.add(1);
        overallSuccess = false;
        console.log(`Delete failed for ${sandboxId}: ${deleteRes.status} - ${deleteRes.body}`);
    }

    failureRate.add(overallSuccess ? 0 : 1);
}

/**
 * Setup function - verify API is accessible
 */
export function setup() {
    const res = http.get(`${BASE_URL}/health`);
    if (res.status !== 200) {
        fail(`API not accessible at ${BASE_URL}: ${res.status}`);
    }
    console.log(`Connected to isola-gw at ${BASE_URL}`);
    return { startTime: Date.now() };
}

/**
 * Teardown function - print summary
 */
export function teardown(data) {
    const duration = (Date.now() - data.startTime) / 1000;
    console.log(`Test completed in ${duration.toFixed(1)}s`);
}

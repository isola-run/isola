/**
 * Sustained Load Test
 *
 * Tests the system under sustained load for extended periods.
 * Helps identify memory leaks, resource exhaustion, and degradation over time.
 *
 * Usage:
 *   k6 run --out json=results.json sustained_load.js
 *
 * For longer runs:
 *   k6 run --out json=results.json -e DURATION=30m sustained_load.js
 */

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter, Gauge } from 'k6/metrics';

// Custom metrics
const createDuration = new Trend('sustained_create_duration_ms');
const readyDuration = new Trend('sustained_ready_duration_ms');
const deleteDuration = new Trend('sustained_delete_duration_ms');
const activeSandboxes = new Gauge('active_sandboxes');
const operationsTotal = new Counter('operations_total');
const failureRate = new Rate('failures');

// Test duration from environment or default
const testDuration = __ENV.DURATION || '10m';

export const options = {
    scenarios: {
        sustained: {
            executor: 'constant-vus',
            vus: 20,
            duration: testDuration,
        },
    },
    thresholds: {
        'sustained_create_duration_ms': ['p(95)<5000'],
        'sustained_ready_duration_ms': ['p(95)<60000'],
        'sustained_delete_duration_ms': ['p(95)<5000'],
        'failures': ['rate<0.02'],  // Stricter for sustained load
    },
};

const BASE_URL = __ENV.ISOLA_API_URL || 'http://localhost:8080';
const API_KEY = __ENV.ISOLA_API_KEY || 'iso_sk_demo';

const headers = {
    'X-API-Key': API_KEY,
    'Content-Type': 'application/json',
};

function waitForReady(sandboxId, timeoutMs = 90000) {
    const startTime = Date.now();

    while (Date.now() - startTime < timeoutMs) {
        const res = http.get(`${BASE_URL}/api/v1/sandboxes/${sandboxId}`, { headers });

        if (res.status === 200) {
            const state = res.json('state');
            if (state === 'running') {
                return { success: true, duration: Date.now() - startTime };
            }
            if (state === 'error' || state === 'failed') {
                return { success: false, duration: Date.now() - startTime };
            }
        }

        sleep(1);
    }

    return { success: false, duration: Date.now() - startTime };
}

export default function() {
    const sandboxName = `sustained-${__VU}-${__ITER}-${Date.now()}`;

    // Create
    const createStart = Date.now();
    const res = http.post(`${BASE_URL}/api/v1/sandboxes`, JSON.stringify({
        name: sandboxName,
        autoStart: true,
        cpu: 0.1,
        memory: 0.128,
    }), { headers });

    createDuration.add(Date.now() - createStart);
    operationsTotal.add(1);

    if (!check(res, { 'create success': (r) => r.status === 201 })) {
        failureRate.add(1);
        return;
    }

    const sandboxId = res.json('id');
    activeSandboxes.add(1);

    // Wait for ready
    const ready = waitForReady(sandboxId);
    readyDuration.add(ready.duration);

    if (!ready.success) {
        failureRate.add(1);
        // Still try to delete
    }

    // Simulate some work
    sleep(Math.random() * 10 + 5);  // 5-15 seconds

    // Delete
    const deleteStart = Date.now();
    const delRes = http.del(`${BASE_URL}/api/v1/sandboxes/${sandboxId}`, null, { headers });
    deleteDuration.add(Date.now() - deleteStart);

    activeSandboxes.add(-1);

    if (!check(delRes, { 'delete success': (r) => r.status === 204 })) {
        failureRate.add(1);
        return;
    }

    failureRate.add(0);
}

export function setup() {
    console.log(`Starting sustained load test for ${testDuration}`);
    return { startTime: Date.now() };
}

export function teardown(data) {
    const duration = (Date.now() - data.startTime) / 1000;
    console.log(`Sustained load test completed in ${duration.toFixed(0)}s`);
}

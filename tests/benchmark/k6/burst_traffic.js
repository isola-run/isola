/**
 * Burst Traffic Load Test
 *
 * Tests the system's ability to handle sudden traffic spikes.
 * Uses constant arrival rate to simulate burst scenarios.
 *
 * Usage:
 *   k6 run --out json=results.json burst_traffic.js
 */

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

// Custom metrics
const burstCreateDuration = new Trend('burst_create_duration_ms');
const burstCreateErrors = new Counter('burst_create_errors');
const rateLimitHits = new Counter('rate_limit_hits');
const failureRate = new Rate('failures');

// Test configuration - constant arrival rate for burst simulation
export const options = {
    scenarios: {
        // Sudden burst of requests
        burst: {
            executor: 'constant-arrival-rate',
            rate: 50,              // 50 requests per second
            timeUnit: '1s',
            duration: '2m',
            preAllocatedVUs: 100,
            maxVUs: 200,
        },
    },
    thresholds: {
        'burst_create_duration_ms': ['p(95)<10000'],  // Allow higher latency during burst
        'failures': ['rate<0.10'],                     // Allow up to 10% failures during burst
        'rate_limit_hits': ['count<100'],             // Expect some rate limiting
    },
};

const BASE_URL = __ENV.ISOLA_API_URL || 'http://localhost:8080';
const API_KEY = __ENV.ISOLA_API_KEY || 'iso_sk_demo';

const headers = {
    'X-API-Key': API_KEY,
    'Content-Type': 'application/json',
};

// Track created sandboxes for cleanup
const createdSandboxes = [];

export default function() {
    const sandboxName = `burst-${__VU}-${__ITER}-${Date.now()}`;

    const createStart = Date.now();
    const createPayload = JSON.stringify({
        name: sandboxName,
        autoStart: true,
        cpu: 0.1,
        memory: 0.128,
    });

    const res = http.post(`${BASE_URL}/api/v1/sandboxes`, createPayload, { headers });
    const createDuration = Date.now() - createStart;
    burstCreateDuration.add(createDuration);

    if (res.status === 429) {
        // Rate limited - expected during burst
        rateLimitHits.add(1);
        failureRate.add(0);  // Don't count rate limiting as failure
        return;
    }

    const success = check(res, {
        'create: status is 201': (r) => r.status === 201,
    });

    if (!success) {
        burstCreateErrors.add(1);
        failureRate.add(1);
        return;
    }

    failureRate.add(0);

    // Store sandbox ID for cleanup
    const sandboxId = res.json('id');

    // Immediately delete to reduce resource pressure during burst test
    sleep(0.5);
    http.del(`${BASE_URL}/api/v1/sandboxes/${sandboxId}`, null, { headers });
}

export function setup() {
    const res = http.get(`${BASE_URL}/health`);
    if (res.status !== 200) {
        throw new Error(`API not accessible at ${BASE_URL}`);
    }
    return { startTime: Date.now() };
}

export function teardown(data) {
    console.log(`Burst test completed in ${((Date.now() - data.startTime) / 1000).toFixed(1)}s`);
}

# Copyright The Isola Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

from __future__ import annotations

import time

import httpx
import pytest
from prometheus_client.parser import text_string_to_metric_families

from utils import ISOLA_METRICS_URL, POLL_INTERVAL


def scrape_metrics() -> dict[str, float]:
    """GET /metrics, parse Prometheus text format, return flat dict keyed by sample name."""
    resp = httpx.get(f"{ISOLA_METRICS_URL}/metrics", timeout=10)
    resp.raise_for_status()
    result: dict[str, float] = {}
    for family in text_string_to_metric_families(resp.text):
        for sample in family.samples:
            result[sample.name] = sample.value
    return result


def scrape_raw() -> str:
    """GET /metrics and return the raw text body."""
    resp = httpx.get(f"{ISOLA_METRICS_URL}/metrics", timeout=10)
    resp.raise_for_status()
    return resp.text


class TestMetrics:
    @pytest.mark.timeout(10)
    def test_metrics_endpoint_reachable(self):
        resp = httpx.get(f"{ISOLA_METRICS_URL}/metrics", timeout=10)
        assert resp.status_code == 200
        assert "# TYPE" in resp.text

    @pytest.mark.timeout(10)
    def test_custom_metrics_registered(self):
        """Spot-check one metric from each controller to verify registration."""
        raw = scrape_raw()
        assert "# TYPE isola_sandbox_created_total " in raw
        assert "# TYPE isola_rootfssnapshot_created_total " in raw

    @pytest.mark.timeout(90)
    def test_sandbox_created_counter_increments(self, isola_client, sandbox_factory):
        before = scrape_metrics().get("isola_sandbox_created_total", 0)

        sb = sandbox_factory(image="alpine:3.21")

        deadline = time.monotonic() + 60
        while time.monotonic() < deadline:
            after = scrape_metrics().get("isola_sandbox_created_total", 0)
            if after >= before + 1:
                return
            time.sleep(POLL_INTERVAL)

        pytest.fail(
            f"isola_sandbox_created_total did not increment within 60s "
            f"(before={before}, after={scrape_metrics().get('isola_sandbox_created_total', 0)})"
        )

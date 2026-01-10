# Tutorial: Data Processing Pipeline

Learn how to build a scalable data processing pipeline using parallel sandboxes. This pattern is useful for ETL jobs, batch processing, and distributed computing.

---

## Overview

In this tutorial, you'll build a pipeline that:

1. Splits data into chunks
2. Processes each chunk in a separate sandbox (parallel)
3. Aggregates results
4. Handles failures gracefully

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Orchestrator                            │
└─────────┬─────────────────────────────────────────────────┬─────┘
          │                                                 │
    ┌─────▼─────┐     ┌───────────┐     ┌───────────┐     │
    │  Sandbox  │     │  Sandbox  │     │  Sandbox  │     │
    │  Chunk 1  │     │  Chunk 2  │     │  Chunk 3  │     │
    └─────┬─────┘     └─────┬─────┘     └─────┬─────┘     │
          │                 │                 │           │
          └─────────────────┴─────────────────┘           │
                            │                             │
                            ▼                             │
                    ┌───────────────┐                     │
                    │   Aggregate   │◀────────────────────┘
                    │    Results    │
                    └───────────────┘
```

---

## Step 1: Create the Processing Template

```yaml
# data-processor-template.yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: SandboxTemplate
metadata:
  name: data-processor
  namespace: isola-sandboxes
spec:
  timeoutSeconds: 300  # 5 minutes per chunk
  podTemplate:
    spec:
      containers:
        - name: sandbox
          image: python:3.11-slim
          command: ["sleep", "infinity"]
          workingDir: /workspace
          resources:
            requests:
              cpu: "500m"
              memory: "512Mi"
            limits:
              cpu: "1000m"
              memory: "1Gi"
```

```bash
kubectl apply -f data-processor-template.yaml
```

---

## Step 2: Build the Pipeline Framework

```python
# pipeline.py
import json
import os
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass, field
from typing import Any, Callable, List, Optional
from isola_client import IsolaClient


@dataclass
class ChunkResult:
    """Result from processing a single chunk."""
    chunk_id: int
    success: bool
    data: Any = None
    error: Optional[str] = None
    duration: float = 0.0


@dataclass
class PipelineResult:
    """Aggregated result from the entire pipeline."""
    success: bool
    total_chunks: int
    successful_chunks: int
    failed_chunks: int
    results: List[ChunkResult] = field(default_factory=list)
    aggregated_data: Any = None
    total_duration: float = 0.0


class DataPipeline:
    """
    Parallel data processing pipeline using Isola sandboxes.
    """

    def __init__(
        self,
        api_url: str,
        api_key: str,
        template: str = "data-processor",
        max_workers: int = 5
    ):
        self.client = IsolaClient(api_url, api_key)
        self.template = template
        self.max_workers = max_workers

    def process(
        self,
        data: List[Any],
        processor_code: str,
        chunk_size: int = 100,
        aggregator: Optional[Callable[[List[Any]], Any]] = None
    ) -> PipelineResult:
        """
        Process data in parallel using sandboxes.

        Args:
            data: List of items to process
            processor_code: Python code to process each chunk
                           (receives 'chunk' variable, must print JSON result)
            chunk_size: Number of items per chunk
            aggregator: Function to aggregate results

        Returns:
            PipelineResult with all chunk results and aggregated data
        """
        start_time = time.time()

        # Split into chunks
        chunks = [
            data[i:i + chunk_size]
            for i in range(0, len(data), chunk_size)
        ]

        print(f"Processing {len(data)} items in {len(chunks)} chunks")

        # Process in parallel
        results = []
        with ThreadPoolExecutor(max_workers=self.max_workers) as executor:
            futures = {
                executor.submit(
                    self._process_chunk,
                    chunk_id,
                    chunk,
                    processor_code
                ): chunk_id
                for chunk_id, chunk in enumerate(chunks)
            }

            for future in as_completed(futures):
                chunk_id = futures[future]
                try:
                    result = future.result()
                    results.append(result)
                    status = "OK" if result.success else "FAILED"
                    print(f"  Chunk {chunk_id}: {status} ({result.duration:.2f}s)")
                except Exception as e:
                    results.append(ChunkResult(
                        chunk_id=chunk_id,
                        success=False,
                        error=str(e)
                    ))

        # Sort by chunk_id for consistent ordering
        results.sort(key=lambda r: r.chunk_id)

        # Aggregate results
        successful_data = [r.data for r in results if r.success and r.data]
        aggregated = aggregator(successful_data) if aggregator else successful_data

        total_duration = time.time() - start_time

        return PipelineResult(
            success=all(r.success for r in results),
            total_chunks=len(chunks),
            successful_chunks=sum(1 for r in results if r.success),
            failed_chunks=sum(1 for r in results if not r.success),
            results=results,
            aggregated_data=aggregated,
            total_duration=total_duration
        )

    def _process_chunk(
        self,
        chunk_id: int,
        chunk: List[Any],
        processor_code: str
    ) -> ChunkResult:
        """Process a single chunk in a sandbox."""
        start_time = time.time()
        sandbox = None

        try:
            # Create sandbox
            sandbox = self.client.create_sandbox(
                name=f"pipeline-chunk-{chunk_id}-{os.urandom(2).hex()}",
                template_name=self.template,
                auto_start=True
            )

            # Wait for ready
            sandbox = self.client.wait_for_state(
                sandbox['id'], "running", timeout=60
            )

            # Upload chunk data
            self.client.upload_file(
                sandbox['id'],
                "/workspace/chunk.json",
                json.dumps(chunk).encode()
            )

            # Upload processor code
            wrapper_code = f'''
import json

# Load chunk data
with open("/workspace/chunk.json") as f:
    chunk = json.load(f)

# User processor code
{processor_code}
'''
            self.client.upload_file(
                sandbox['id'],
                "/workspace/process.py",
                wrapper_code.encode()
            )

            # Execute
            result = self.client.execute_command(
                sandbox['id'],
                "python /workspace/process.py",
                timeout=240
            )

            duration = time.time() - start_time

            if result['exitCode'] != 0:
                return ChunkResult(
                    chunk_id=chunk_id,
                    success=False,
                    error=result['stderr'],
                    duration=duration
                )

            # Parse output as JSON
            try:
                data = json.loads(result['stdout'])
            except json.JSONDecodeError:
                data = result['stdout']

            return ChunkResult(
                chunk_id=chunk_id,
                success=True,
                data=data,
                duration=duration
            )

        except Exception as e:
            return ChunkResult(
                chunk_id=chunk_id,
                success=False,
                error=str(e),
                duration=time.time() - start_time
            )

        finally:
            if sandbox:
                try:
                    self.client.terminate_sandbox(sandbox['id'])
                except Exception:
                    pass
```

---

## Step 3: Use the Pipeline

### Example 1: Number Processing

```python
# example_numbers.py
from pipeline import DataPipeline

pipeline = DataPipeline(
    api_url="http://localhost:8080",
    api_key="iso_sk_demo",
    max_workers=5
)

# Data to process
numbers = list(range(1, 1001))  # 1 to 1000

# Processor code (runs in sandbox)
processor = '''
# Calculate sum and statistics for this chunk
result = {
    "sum": sum(chunk),
    "count": len(chunk),
    "min": min(chunk),
    "max": max(chunk),
    "squares_sum": sum(x**2 for x in chunk)
}
print(json.dumps(result))
'''

# Aggregator function (runs locally)
def aggregate(results):
    return {
        "total_sum": sum(r["sum"] for r in results),
        "total_count": sum(r["count"] for r in results),
        "global_min": min(r["min"] for r in results),
        "global_max": max(r["max"] for r in results),
        "total_squares": sum(r["squares_sum"] for r in results)
    }

# Run pipeline
result = pipeline.process(
    data=numbers,
    processor_code=processor,
    chunk_size=100,
    aggregator=aggregate
)

print(f"\nPipeline completed in {result.total_duration:.2f}s")
print(f"Chunks: {result.successful_chunks}/{result.total_chunks} successful")
print(f"Results: {result.aggregated_data}")
```

**Output:**
```
Processing 1000 items in 10 chunks
  Chunk 0: OK (2.34s)
  Chunk 3: OK (2.41s)
  Chunk 1: OK (2.45s)
  ...

Pipeline completed in 5.23s
Chunks: 10/10 successful
Results: {'total_sum': 500500, 'total_count': 1000, 'global_min': 1, 'global_max': 1000, 'total_squares': 333833500}
```

---

### Example 2: Text Analysis

```python
# example_text.py
from pipeline import DataPipeline

pipeline = DataPipeline(
    api_url="http://localhost:8080",
    api_key="iso_sk_demo"
)

# Documents to analyze
documents = [
    "The quick brown fox jumps over the lazy dog.",
    "Python is a powerful programming language.",
    "Kubernetes orchestrates containerized applications.",
    # ... more documents
] * 50  # 150 documents

# Processor: word frequency analysis
processor = '''
from collections import Counter
import re

all_words = []
for doc in chunk:
    words = re.findall(r'\\w+', doc.lower())
    all_words.extend(words)

word_counts = Counter(all_words)
result = {
    "total_words": len(all_words),
    "unique_words": len(word_counts),
    "top_10": word_counts.most_common(10)
}
print(json.dumps(result))
'''

def aggregate(results):
    from collections import Counter
    combined = Counter()
    total_words = 0
    for r in results:
        total_words += r["total_words"]
        combined.update(dict(r["top_10"]))
    return {
        "total_words": total_words,
        "top_words": combined.most_common(20)
    }

result = pipeline.process(
    data=documents,
    processor_code=processor,
    chunk_size=30,
    aggregator=aggregate
)

print(f"Analyzed {result.aggregated_data['total_words']} words")
print(f"Top words: {result.aggregated_data['top_words'][:5]}")
```

---

### Example 3: Image Processing (with network access)

For processing that requires downloading files, create a network-enabled template:

```yaml
# image-processor-template.yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: SandboxTemplate
metadata:
  name: image-processor
  namespace: isola-sandboxes
spec:
  timeoutSeconds: 600
  podTemplate:
    spec:
      containers:
        - name: sandbox
          image: python:3.11
          command: ["sleep", "infinity"]
          resources:
            limits:
              cpu: "2000m"
              memory: "2Gi"
---
apiVersion: sandbox.isola.run/v1alpha1
kind: NetworkTemplate
metadata:
  name: image-download
  namespace: isola-sandboxes
spec:
  allowedEgress:
    - "0.0.0.0/0"  # Allow downloading images
  dnsServers:
    - "8.8.8.8"
```

```python
# example_images.py
from pipeline import DataPipeline

pipeline = DataPipeline(
    api_url="http://localhost:8080",
    api_key="iso_sk_demo",
    template="image-processor"
)

# Image URLs to process
image_urls = [
    "https://example.com/image1.jpg",
    "https://example.com/image2.jpg",
    # ...
]

processor = '''
import urllib.request
import hashlib

results = []
for url in chunk:
    try:
        # Download image
        with urllib.request.urlopen(url, timeout=30) as response:
            data = response.read()

        # Calculate hash and size
        results.append({
            "url": url,
            "size": len(data),
            "hash": hashlib.md5(data).hexdigest(),
            "success": True
        })
    except Exception as e:
        results.append({
            "url": url,
            "success": False,
            "error": str(e)
        })

print(json.dumps(results))
'''

result = pipeline.process(
    data=image_urls,
    processor_code=processor,
    chunk_size=10
)
```

---

## Step 4: Error Handling and Retries

```python
# pipeline_with_retries.py
from pipeline import DataPipeline, PipelineResult, ChunkResult
from typing import List, Any, Callable, Optional


class RobustPipeline(DataPipeline):
    """Pipeline with retry logic for failed chunks."""

    def process_with_retry(
        self,
        data: List[Any],
        processor_code: str,
        chunk_size: int = 100,
        aggregator: Optional[Callable] = None,
        max_retries: int = 2
    ) -> PipelineResult:
        """Process with automatic retry for failed chunks."""

        # First pass
        result = self.process(data, processor_code, chunk_size, aggregator)

        if result.success:
            return result

        # Retry failed chunks
        for retry in range(max_retries):
            failed_chunks = [
                r for r in result.results if not r.success
            ]

            if not failed_chunks:
                break

            print(f"\nRetry {retry + 1}: {len(failed_chunks)} failed chunks")

            # Get original data for failed chunks
            chunks = [
                data[i * chunk_size:(i + 1) * chunk_size]
                for i in range(0, len(data), chunk_size)
            ]

            # Retry each failed chunk
            for failed in failed_chunks:
                chunk_data = chunks[failed.chunk_id]
                new_result = self._process_chunk(
                    failed.chunk_id,
                    chunk_data,
                    processor_code
                )

                # Update result
                idx = next(
                    i for i, r in enumerate(result.results)
                    if r.chunk_id == failed.chunk_id
                )
                result.results[idx] = new_result

        # Recalculate aggregation
        successful_data = [r.data for r in result.results if r.success]
        result.aggregated_data = aggregator(successful_data) if aggregator else successful_data
        result.successful_chunks = sum(1 for r in result.results if r.success)
        result.failed_chunks = sum(1 for r in result.results if not r.success)
        result.success = result.failed_chunks == 0

        return result
```

---

## Step 5: Monitoring and Progress

```python
# pipeline_monitored.py
import threading
from dataclasses import dataclass
from typing import Callable


@dataclass
class PipelineProgress:
    total_chunks: int
    completed_chunks: int
    successful_chunks: int
    failed_chunks: int
    current_chunk: int


class MonitoredPipeline(DataPipeline):
    """Pipeline with progress callbacks."""

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self._progress_callback = None
        self._lock = threading.Lock()
        self._completed = 0
        self._successful = 0
        self._failed = 0

    def on_progress(self, callback: Callable[[PipelineProgress], None]):
        """Set progress callback."""
        self._progress_callback = callback
        return self

    def _process_chunk(self, chunk_id, chunk, processor_code):
        result = super()._process_chunk(chunk_id, chunk, processor_code)

        with self._lock:
            self._completed += 1
            if result.success:
                self._successful += 1
            else:
                self._failed += 1

            if self._progress_callback:
                self._progress_callback(PipelineProgress(
                    total_chunks=self._total_chunks,
                    completed_chunks=self._completed,
                    successful_chunks=self._successful,
                    failed_chunks=self._failed,
                    current_chunk=chunk_id
                ))

        return result


# Usage
def print_progress(p: PipelineProgress):
    pct = (p.completed_chunks / p.total_chunks) * 100
    print(f"\r[{pct:5.1f}%] {p.completed_chunks}/{p.total_chunks} "
          f"(OK: {p.successful_chunks}, FAIL: {p.failed_chunks})", end="")


pipeline = MonitoredPipeline(
    api_url="http://localhost:8080",
    api_key="iso_sk_demo"
).on_progress(print_progress)
```

---

## Performance Tips

1. **Tune chunk size** - Larger chunks = fewer sandboxes but longer per-chunk time
2. **Adjust max_workers** - Match to cluster capacity
3. **Use appropriate resources** - Don't over-provision sandbox resources
4. **Pre-warm sandboxes** - For latency-sensitive workloads, keep sandboxes ready
5. **Monitor cluster** - Watch for resource exhaustion

---

## Next Steps

- [Multi-tenant SaaS](./multi-tenant.md) - Isolate customer workloads
- [CI/CD Integration](./ci-cd-integration.md) - Use in build pipelines
- [Configuration Guide](../configuration.md) - Optimize templates

#!/usr/bin/env python3
"""Quickstart: The simplest way to use Isola.

This is the minimal example to get started with Isola.

The example defaults to the dev environment settings:
- Base URL: http://localhost:30080 (Tilt port forwarding)
- API Key: iso_sk_demo

You can override these via environment variables:
    export ISOLA_BASE_URL="http://localhost:30080"
    export ISOLA_API_KEY="iso_sk_demo"

Usage:
    python quickstart.py
"""

import isola
import os

# Create sandbox configuration
# For dev environment, default to port 30080 (Tilt port forwarding)
# You can override via ISOLA_BASE_URL env var or pass explicitly
base_url = os.environ.get("ISOLA_BASE_URL") or "http://localhost:30080"
api_key = os.environ.get("ISOLA_API_KEY") or "iso_sk_demo"
sandbox = (
    isola.Sandbox("quickstart", base_url=base_url, api_key=api_key)
    .image("python:3.11-slim")
)


# Execute in sandbox using context manager
if __name__ == "__main__":
    with sandbox.run() as session:
        result = session.exec('python -c "print(\'Hello, World! I am running in a sandbox.\')"')
        print(result.stdout)

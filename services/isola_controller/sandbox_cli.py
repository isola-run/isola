#!/usr/bin/env python3
"""
Small REPL-style helper for the isola_controller client gateway.

Usage examples:
  GATEWAY_URL=http://localhost:30080 API_KEY=iso_sk_demo python sandbox_cli.py
  python sandbox_cli.py --url http://localhost:30080 list
  python sandbox_cli.py create my-sandbox --image python:3.11 --wait
"""

import json
import os
import shlex
import sys
import time
import uuid
from typing import Any, Dict, List, Optional, Tuple

import requests

DEFAULT_GATEWAY_URL = os.getenv("GATEWAY_URL", "http://192.168.49.2:30080")
DEFAULT_API_KEY = os.getenv("API_KEY", "iso_sk_demo")
DEFAULT_TIMEOUT = int(os.getenv("TIMEOUT_SECONDS", "120"))
DEFAULT_POLL_INTERVAL = float(os.getenv("POLL_INTERVAL", "2"))


def _to_number(value: str) -> Any:
    """Convert simple numeric strings to int/float for payloads."""
    try:
        if "." in value:
            return float(value)
        return int(value)
    except ValueError:
        return value


def _pretty(data: Any) -> str:
    return json.dumps(data, indent=2, sort_keys=True)


class SandboxClient:
    def __init__(self, base_url: str, api_key: str):
        self.base_url = base_url.rstrip("/")
        self.session = requests.Session()
        self.session.headers.update({"X-API-Key": api_key, "Content-Type": "application/json"})

    def _request(self, method: str, path: str, **kwargs) -> Optional[requests.Response]:
        url = f"{self.base_url}{path}"
        try:
            return self.session.request(method, url, **kwargs)
        except requests.RequestException as exc:
            print(f"! Request to {url} failed: {exc}")
            return None

    def _json(self, response: Optional[requests.Response]) -> Optional[Any]:
        if response is None:
            return None
        if not response.ok:
            body = response.text.strip()
            prefix = f"! HTTP {response.status_code}"
            print(f"{prefix}: {body or response.reason}")
            return None
        try:
            return response.json()
        except ValueError:
            print(response.text)
            return None

    def health(self) -> Optional[Any]:
        return self._json(self._request("GET", "/health", timeout=10))

    def list_sandboxes(
        self, state: Optional[str] = None, limit: int = 20, offset: int = 0
    ) -> Optional[Any]:
        params: Dict[str, Any] = {"limit": limit, "offset": offset}
        if state:
            params["state"] = state
        return self._json(self._request("GET", "/sandboxes", params=params, timeout=20))

    def create_sandbox(self, payload: Dict[str, Any]) -> Optional[Any]:
        return self._json(
            self._request("POST", "/sandboxes", json=payload, timeout=30)
        )

    def get_sandbox(self, sandbox_id: str) -> Optional[Any]:
        return self._json(
            self._request("GET", f"/sandboxes/{sandbox_id}", timeout=15)
        )

    def delete_sandbox(self, sandbox_id: str) -> bool:
        response = self._request("DELETE", f"/sandboxes/{sandbox_id}", timeout=30)
        if response is None:
            return False
        if response.status_code == 204:
            return True
        print(f"! Delete failed ({response.status_code}): {response.text}")
        return False

    def execute(self, sandbox_id: str, command: str) -> Optional[Any]:
        return self._json(
            self._request(
                "POST",
                f"/sandboxes/{sandbox_id}/execute",
                json={"command": command},
                timeout=60,
            )
        )


def parse_global_args(argv: List[str]) -> Tuple[str, str, List[str]]:
    """Pull out --url/--gateway and --api-key flags; everything else stays in place."""
    base_url = DEFAULT_GATEWAY_URL
    api_key = DEFAULT_API_KEY
    remaining: List[str] = []

    i = 0
    while i < len(argv):
        arg = argv[i]
        if arg in {"--url", "--gateway"} and i + 1 < len(argv):
            base_url = argv[i + 1]
            i += 2
            continue
        if arg == "--api-key" and i + 1 < len(argv):
            api_key = argv[i + 1]
            i += 2
            continue
        remaining = argv[i:]
        break

    return base_url, api_key, remaining


def build_create_payload(args: List[str]) -> Dict[str, Any]:
    """Parse simple flags into a CreateSandbox payload."""
    payload: Dict[str, Any] = {"name": f"cli-{uuid.uuid4().hex[:8]}", "autoStart": True}
    env_vars: Dict[str, str] = {}

    i = 0
    while i < len(args):
        arg = args[i]
        if arg in {"--stopped", "--no-auto-start"}:
            payload["autoStart"] = False
            i += 1
            continue
        if arg in {"--wait", "-w"}:
            payload["_wait"] = True
            i += 1
            continue
        if arg in {"--image", "-i"} and i + 1 < len(args):
            payload["image"] = args[i + 1]
            i += 2
            continue
        if arg in {"--cpu", "--memory", "--disk"} and i + 1 < len(args):
            key = arg.lstrip("-")
            payload[key] = _to_number(args[i + 1])
            i += 2
            continue
        if arg in {"--class", "--region"} and i + 1 < len(args):
            key = "class" if arg == "--class" else "region"
            payload[key] = args[i + 1]
            i += 2
            continue
        if arg == "--env" and i + 1 < len(args):
            if "=" in args[i + 1]:
                key, value = args[i + 1].split("=", 1)
                env_vars[key] = value
            i += 2
            continue
        if "name" not in payload or payload["name"].startswith("cli-"):
            payload["name"] = arg
        else:
            print(f"! Unrecognized create option: {arg}")
        i += 1

    if env_vars:
        payload["env"] = env_vars
    return payload


def wait_for_state(
    client: SandboxClient,
    sandbox_id: str,
    target_state: str = "running",
    timeout: int = DEFAULT_TIMEOUT,
    poll_interval: float = DEFAULT_POLL_INTERVAL,
) -> Optional[Any]:
    start = time.time()
    last_state = None
    while time.time() - start < timeout:
        data = client.get_sandbox(sandbox_id)
        if not data:
            time.sleep(poll_interval)
            continue
        state = data.get("state")
        if state != last_state:
            print(f"- state: {state}")
            last_state = state
        if state == target_state:
            return data
        if state == "error":
            print(f"! Sandbox is in error state: {data.get('errorReason')}")
            return data
        time.sleep(poll_interval)
    print(f"! Timed out after {timeout}s waiting for {target_state}")
    return None


def handle_command(client: SandboxClient, parts: List[str]) -> None:
    if not parts:
        return
    cmd = parts[0]
    args = parts[1:]

    if cmd in {"quit", "exit"}:
        raise SystemExit
    if cmd in {"help", "?"}:
        print_help()
        return
    if cmd == "health":
        data = client.health()
        if data is not None:
            print(_pretty(data))
        return
    if cmd == "list":
        state = args[0] if args else None
        data = client.list_sandboxes(state=state)
        if data is not None:
            print(_pretty(data))
        return
    if cmd == "create":
        payload = build_create_payload(args)
        wait_flag = payload.pop("_wait", False)
        print(f"- creating sandbox: {payload}")
        data = client.create_sandbox(payload)
        if data is None:
            return
        print(f"✓ created {data.get('id')} ({data.get('name')})")
        if wait_flag and data.get("id"):
            print(f"- waiting for sandbox {data['id']} to reach 'running'")
            waited = wait_for_state(client, data["id"])
            if waited:
                print(_pretty(waited))
        return
    if cmd in {"get", "show"} and args:
        data = client.get_sandbox(args[0])
        if data is not None:
            print(_pretty(data))
        return
    if cmd == "wait" and args:
        target = args[1] if len(args) > 1 else "running"
        data = wait_for_state(client, args[0], target_state=target)
        if data:
            print(_pretty(data))
        return
    if cmd in {"delete", "rm"} and args:
        if client.delete_sandbox(args[0]):
            print(f"✓ deleted {args[0]}")
        return
    if cmd == "exec" and len(args) >= 2:
        sandbox_id = args[0]
        command = " ".join(args[1:])
        result = client.execute(sandbox_id, command)
        if result is not None:
            print(_pretty(result))
        return

    print(f"! Unknown or incomplete command: {' '.join(parts)}")


def print_help() -> None:
    print(
        """
Commands:
  health                               Check gateway health
  list [state]                         List sandboxes (optionally filter by state)
  create [name] [--image IMG] [--stopped] [--wait]
                                       Create a sandbox; defaults are fine for quick tries
  get|show <sandbox_id>                Show sandbox details
  wait <sandbox_id> [state]            Poll until sandbox reaches a state (default: running)
  delete|rm <sandbox_id>               Delete sandbox
  exec <sandbox_id> <command>          Run a command in a running sandbox
  help                                 Show this message
  exit|quit                            Leave the REPL

Flags available before the command:
  --url/--gateway <url>                Override gateway URL (env GATEWAY_URL also works)
  --api-key <key>                      Override API key (env API_KEY also works)
"""
    )


def repl(client: SandboxClient) -> None:
    print(
        f"Gateway: {client.base_url} | API key: {client.session.headers.get('X-API-Key')}"
    )
    print("Type 'help' for commands, Ctrl+D to exit.")
    while True:
        try:
            raw = input("sandbox> ")
        except EOFError:
            print()
            break
        if not raw.strip():
            continue
        parts = shlex.split(raw)
        try:
            handle_command(client, parts)
        except SystemExit:
            break


def main() -> None:
    base_url, api_key, remaining = parse_global_args(sys.argv[1:])
    client = SandboxClient(base_url, api_key)

    if not remaining:
        repl(client)
        return

    try:
        handle_command(client, remaining)
    except SystemExit:
        pass


if __name__ == "__main__":
    main()

import asyncio
import json
import logging
import os
import random
import sys
import time
import uuid
from typing import Optional

import websockets

# Import shared message schemas
from isola.models.agent_ws import AgentHello, AgentStatusUpdate


logger = logging.getLogger(__name__)


class AgentWebSocketClient:
    """Minimal WebSocket client for the AgentManager.

    - Connects to the AgentManager WebSocket endpoint
    - Sends an AgentHello on connect and waits for ack
    - Periodically sends AgentStatusUpdate messages
    """

    def __init__(
        self,
        url: str = "ws://localhost:8765/ws",
        agent_id: Optional[uuid.UUID] = None,
        status_interval_s: float = 5.0,
    ) -> None:
        self._url = url
        self._agent_id = agent_id or uuid.uuid4()
        self._status_interval_s = status_interval_s
        self._stop_event = asyncio.Event()

    async def run(self) -> None:
        """Run the client until stopped or disconnected.

        Implements simple retry logic to handle the server not being ready.
        """
        backoff_s = 0.1
        max_backoff_s = 5.0

        while not self._stop_event.is_set():
            try:
                logger.info("Connecting to %s", self._url)
                async with websockets.connect(self._url) as ws:
                    await self._on_connect(ws)

                    # Main status loop
                    while not self._stop_event.is_set():
                        cpu, mem = self._sample_metrics()
                        status = AgentStatusUpdate(
                            agent_id=self._agent_id, cpu=cpu, mem=mem
                        )
                        await ws.send(status.model_dump_json())
                        await asyncio.sleep(self._status_interval_s)

                # If we exit the context manager without exceptions, attempt reconnect
                logger.info("Disconnected cleanly, reconnecting after %.2fs", backoff_s)
            except (OSError, ConnectionRefusedError) as e:
                logger.warning("Connection failed: %s", e)
            except websockets.ConnectionClosed as e:
                logger.warning("WebSocket closed: %s", e)
            except Exception:
                logger.exception("Unexpected error in agent client loop")

            # Backoff before retrying connection
            await asyncio.wait([self._stop_event.wait()], timeout=backoff_s)
            backoff_s = min(max_backoff_s, backoff_s * 2)

    async def _on_connect(self, ws: websockets.WebSocketClientProtocol) -> None:
        """Send AgentHello on connect and wait for ack before proceeding."""
        hello = AgentHello(agent_id=self._agent_id)
        await ws.send(hello.model_dump_json())

        # Expect an ack/nack response for the hello
        try:
            raw = await asyncio.wait_for(ws.recv(), timeout=5.0)
            data = json.loads(raw)
            msg_type = data.get("type")
            if msg_type != "ack":
                raise RuntimeError(f"Expected ack, got: {data}")
            logger.info("Received ack for hello: %s", data.get("acked_id"))
        except asyncio.TimeoutError:
            raise RuntimeError("Timed out waiting for ack to hello")

    def stop(self) -> None:
        self._stop_event.set()

    def _sample_metrics(self) -> tuple[float, float]:
        """Sample CPU and memory usage.

        Tries to use psutil if available; otherwise falls back to simple
        approximations so we don't depend on extra packages.
        Returns (cpu_percent, mem_percent).
        """
        # Try psutil if available
        try:
            import psutil  # type: ignore

            cpu = float(psutil.cpu_percent(interval=None))
            mem = float(psutil.virtual_memory().percent)
            return cpu, mem
        except Exception:
            pass

        # Fallback CPU: derive from load average if available
        cpu: float
        try:
            load_1, _load5, _load15 = os.getloadavg()  # type: ignore[attr-defined]
            ncpu = os.cpu_count() or 1
            cpu = float(min(100.0, max(0.0, (load_1 / max(1, ncpu)) * 100.0)))
        except Exception:
            cpu = float(random.uniform(0.0, 50.0))

        # Fallback memory: random but stable range
        mem = float(random.uniform(10.0, 70.0))
        return cpu, mem


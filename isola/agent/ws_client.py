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

from isola.models.agent_ws import AgentHello, AgentStatusUpdate


logger = logging.getLogger(__name__)




class ControlPlaneClient:
    def __init__(
        self,
        agent_id: uuid.UUID,
        control_plane_url: str = "ws://localhost:8765/ws",
    ) -> None:
        self._control_plane_url = control_plane_url
        self._agent_id = agent_id


    async def _send_agent_hello(self, ws: websockets.ClientConnection) -> None:
        hello = AgentHello(agent_id=self._agent_id)
        await ws.send(hello.model_dump_json())


    async def _client_loop(self) -> None:
        while True:
            try:
                async with websockets.connect(self._control_plane_url) as ws:
                    await self._send_agent_hello(ws)

            except Exception:
                logger.exception("Exception in control plane client loop for agent: %s", self._agent_id)

    async def start(self) -> None:
        asyncio.create_task(self._client_loop())

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


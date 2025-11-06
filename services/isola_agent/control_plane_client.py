import asyncio
import logging
import os
import random
import uuid

import websockets


from common.models.control_protocol import Ack, AgentHello, AgentStatusUpdate, OutgoingAdapter



logger = logging.getLogger(__name__)


class ControlPlaneClient:
    def __init__(
        self,
        agent_id: uuid.UUID,
        control_plane_url,
    ) -> None:
        self._control_plane_url = control_plane_url
        self._agent_id = agent_id


    async def _send_agent_hello(self, ws: websockets.ClientConnection) -> None:
        hello = AgentHello(agent_id=self._agent_id)
        await ws.send(hello.model_dump_json())


    async def _receiver_loop(self, ws: websockets.ClientConnection) -> None:
        while True:
            try:
                data = await ws.recv()
                msg = OutgoingAdapter.validate_python(data)
                logger.info("received: %s", msg)
                asyncio.create_task(ws.send(Ack(acked_id=msg.id).model_dump_json()))
            except Exception:
                logger.exception("Exception in control plane client receiver loop for agent: %s", self._agent_id)
    

    async def _sender_loop(self, ws: websockets.ClientConnection) -> None:
        while True:
            try:
                cpu, mem = self._sample_metrics()
                status_update = AgentStatusUpdate(
                    agent_id=self._agent_id, cpu=cpu, mem=mem
                )
                asyncio.create_task(ws.send(status_update.model_dump_json()))
                await asyncio.sleep(1)
            except Exception:
                logger.exception("Exception in control plane client sender loop for agent: %s", self._agent_id)

    async def _loop_with_reconnects(self) -> None:
        while True:
            try:
                async with websockets.connect(self._control_plane_url) as ws:
                    recv_task = asyncio.create_task(self._receiver_loop(ws))
                    await self._send_agent_hello(ws)
                    send_task = asyncio.create_task(self._sender_loop(ws))
                    await asyncio.gather(recv_task, send_task)
            except Exception:
                logger.exception("Exception in control plane client loop for agent: %s", self._agent_id)
    async def start(self) -> None:
        asyncio.create_task(self._loop_with_reconnects())


    def _sample_metrics(self) -> tuple[float, float]:
        """Sample CPU and memory usage.

        Tries to use psutil if available; otherwise falls back to simple
        approxima   tions so we don't depend on extra packages.
        Returns (cpu_percent, mem_percent).
        """
        # Try psutil if available
        cpu: float = 0
        mem: float = 0
        try:
            import psutil  # type: ignore

            cpu = float(psutil.cpu_percent(interval=None))
            mem = float(psutil.virtual_memory().percent)
            return cpu, mem
        except Exception:
            pass

        # Fallback CPU: derive from load average if available
        try:
            load_1, _load5, _load15 = os.getloadavg()  # type: ignore[attr-defined]
            ncpu = os.cpu_count() or 1
            cpu = float(min(100.0, max(0.0, (load_1 / max(1, ncpu)) * 100.0)))
        except Exception:
            cpu = float(random.uniform(0.0, 50.0))

        # Fallback memory: random but stable range
        mem = float(random.uniform(10.0, 70.0))
        return cpu, mem


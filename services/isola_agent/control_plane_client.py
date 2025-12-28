import asyncio
import json
import logging
import os
import random
import uuid

import websockets
from websockets.exceptions import ConnectionClosedError, ConnectionClosedOK

# Suppress verbose websocket debug logs
for ws_logger in ["websockets", "websockets.client", "websockets.server", "websockets.protocol"]:
    logging.getLogger(ws_logger).setLevel(logging.WARNING)

from common.models.control_protocol import Ack, AgentHello, AgentStatusUpdate, CreateSandboxRequest, CreateSandboxResponse, Nack, OutgoingAdapter



logger = logging.getLogger(__name__)


class ControlPlaneClient:
    def __init__(
        self,
        agent_id: uuid.UUID,
        control_plane_url,
    ) -> None:
        self._control_plane_url = control_plane_url
        self._agent_id = agent_id
        self._current_ws: websockets.ClientConnection | None = None
        self._send_lock: asyncio.Lock = asyncio.Lock()


    async def _verify_solid_ground(self) -> websockets.ClientConnection:
        async with self._send_lock:
            needs_hello = False
            if self._current_ws is None:
                self._current_ws = await websockets.connect(self._control_plane_url)
                needs_hello = True
            if self._current_ws.close_code is not None:
                logger.warning("current ws has close_code %s, will reconnect", self._current_ws.close_code)
                self._current_ws = await websockets.connect(self._control_plane_url)
                needs_hello = True
                
            if needs_hello:
                hello = AgentHello(agent_id=self._agent_id)
                logger.info("Sending agent hello to control plane: %s", hello)
                await self._current_ws.send(hello.model_dump_json())
            return self._current_ws
            


    async def _fire_and_forget_send(self, payload: str) -> None:
        logger.debug("sending: %s", payload)
        asyncio.create_task(self._send_via_current_ws(payload))


    async def _send_via_current_ws(self, payload: str) -> None:
        try:
            ws = await self._verify_solid_ground()
            await ws.send(payload)
        except (ConnectionClosedError, ConnectionClosedOK):
            logger.warning(
                "Send aborted due to closed control plane connection for agent %s",
                self._agent_id,
            )
            raise
        except Exception:
            logger.exception(
                "Unexpected error sending message to control plane for agent: %s",
                self._agent_id,
            )
            raise


    async def _receiver_loop(self) -> None:
        while True:
            try:
                ws = await self._verify_solid_ground()
                raw = await ws.recv()
                data = json.loads(raw)
                msg = OutgoingAdapter.validate_python(data)
                logger.debug("received: %s", msg)
                if isinstance(msg, Ack) or isinstance(msg, Nack):
                    pass
                elif isinstance(msg, CreateSandboxRequest):
                    await self._fire_and_forget_send(
                        CreateSandboxResponse(
                            sandbox_id=msg.sandbox_id, success=True
                        ).model_dump_json()
                    )
                else:
                    await self._fire_and_forget_send(Ack(acked_id=msg.id).model_dump_json())
            except Exception:
                logger.exception("Exception in control plane client receiver loop for agent: %s", self._agent_id)
                await asyncio.sleep(1)
    

    async def _sender_loop(self) -> None:
        while True:
            try:
                cpu, mem = self._sample_metrics()
                status_update = AgentStatusUpdate(
                    agent_id=self._agent_id, cpu=cpu, mem=mem
                )
                await self._fire_and_forget_send(status_update.model_dump_json())
                await asyncio.sleep(1)
            except Exception:
                logger.exception("Exception in control plane client sender loop for agent: %s", self._agent_id)
                await asyncio.sleep(1)

    async def _loop(self) -> None:
        while True:
            try:
                logger.info("Entering control plane client loop for agent: %s", self._agent_id)
                recv_task = asyncio.create_task(self._receiver_loop())
                send_task = asyncio.create_task(self._sender_loop())
                await asyncio.gather(recv_task, send_task)
                logger.info("Ended control plane client loop for agent: %s", self._agent_id)
            except Exception:
                logger.exception("Exception in control plane client loop for agent: %s", self._agent_id)
                await asyncio.sleep(1)

    async def start(self) -> None:
        asyncio.create_task(self._loop())


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

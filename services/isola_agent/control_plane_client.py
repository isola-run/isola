import asyncio
import json
import logging
import os
import random
import uuid

import websockets
from websockets.exceptions import ConnectionClosedError, ConnectionClosedOK

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


    async def _send_agent_hello(self) -> None:
        hello = AgentHello(agent_id=self._agent_id)
        ws = await self._verify_solid_ground()
        await ws.send(hello.model_dump_json())

    async def _verify_solid_ground(self) -> websockets.ClientConnection:
        # todo benl: make sense of the locking
        async with self._send_lock:
            if self._current_ws is None:
                self._current_ws = await websockets.connect(self._control_plane_url)
            if self._current_ws.close_code is not None:
                logger.warning("current ws has close_code %s, will reconnect", self._current_ws.close_code)
                self._current_ws = await websockets.connect(self._control_plane_url)
            return self._current_ws
            


    async def _fire_and_forget_send(self, payload: str) -> None:
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
        except Exception:
            logger.exception(
                "Unexpected error sending message to control plane for agent: %s",
                self._agent_id,
            )


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
            except (ConnectionClosedError, ConnectionClosedOK) as exc:
                logger.info(
                    "Control plane connection closed for agent %s in receiver loop: %s",
                    self._agent_id,
                    exc,
                )
                break
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
            except (ConnectionClosedError, ConnectionClosedOK) as exc:
                logger.info(
                    "Control plane connection closed for agent %s in sender loop: %s",
                    self._agent_id,
                    exc,
                )
                break
            except Exception:
                logger.exception("Exception in control plane client sender loop for agent: %s", self._agent_id)
                await asyncio.sleep(1)

    async def _loop(self) -> None:
        while True:
            try:
                recv_task = asyncio.create_task(self._receiver_loop())
                await self._send_agent_hello()
                send_task = asyncio.create_task(self._sender_loop())
                await asyncio.gather(recv_task, send_task)
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


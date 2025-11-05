import logging
from pydantic import ValidationError
import uvicorn
import asyncio
import time
import uuid
import json
from fastapi import FastAPI, WebSocket, WebSocketDisconnect
from dataclasses import dataclass


from isola.models.agent_ws import (
    Ack,
    Nack,
    AgentHello,
    AgentStatusUpdate,
    IncomingAdapter,
)

logger = logging.getLogger()

@dataclass
class AgentStatus:
    last_activity: int
    # add n_active_ws from agent to count multiple ws (e.g. due to bugs or by design?)
    # should probably keep several samples / equation for trend:
    last_cpu: float
    last_mem: float

class AgentManager:
    def __init__(self) -> None:
        self._active_agents: dict[uuid.UUID, AgentStatus] = {}
        self._app = FastAPI()
        # todo benl: properly config
        config = uvicorn.Config(self._app, host="localhost", port=8765, log_level="debug")
        self._server = uvicorn.Server(config)
        self._server_task: asyncio.TTask | None = None
        self._app.add_api_websocket_route("/ws", self._manager_loop)
    
    async def _manager_loop(self, ws: WebSocket) -> None:
        await ws.accept()

        # agent_id -> ws
        agent_id: uuid.UUID | None = None
        try:
            while True:
                raw = await ws.receive_text()
                try:
                    data = json.loads(raw)
                    msg = IncomingAdapter.validate_python(data)
                except (json.JSONDecodeError, ValidationError):
                    logger.exception("Failed to validate incoming message: %s", raw)
                    continue

                # no server-side deduplication yet

                if agent_id:
                    if isinstance(msg, AgentStatusUpdate):
                        self._active_agents[msg.agent_id].last_activity = msg.ts
                        self._active_agents[msg.agent_id].last_cpu = msg.cpu
                        self._active_agents[msg.agent_id].last_mem = msg.mem
                    else:
                        ack = Ack(acked_id=msg.id)
                        await ws.send_text(ack.model_dump_json())
                else: # not identified yet - ~first message
                    if isinstance(msg, AgentHello):
                        logger.info("identified agent: %s", msg.agent_id)
                        ack = Ack(acked_id=msg.id)
                        await ws.send_text(ack.model_dump_json())
                        agent_id = msg.agent_id
                        # todo benl: better sentinel values
                        self._active_agents[msg.agent_id] = AgentStatus(
                            last_activity=msg.ts,
                            last_cpu=-1,
                            last_mem=-1,
                        )
                    else:
                        logger.warning("first message from agent is not hello, got: %s", msg)
                        # todo benl: what if message is not (n)ackable?
                        nack = Nack(nacked_id=msg.id)
                        await ws.send_text(nack.model_dump_json())
                            
        except WebSocketDisconnect:
            logger.warning("Agent disconnected: %s", agent_id)
        except Exception:
            logger.exception("Unknown exception, agent_id: %s", agent_id)
    
        # how would this close gracefully?
        logger.info("websocket loop exiting for agent: %s", agent_id)

    async def start(self) -> None:
        logger.info("Starting agent manager server on localhost:8765...")
        # Run the server in a background task
        self._server_task = asyncio.create_task(self._server.serve())

    async def shutdown(self) -> None:
        if self._server_task:
            logger.info("Shutting agent manager server down...")
            await self._server.shutdown()
            logger.info("agent manager shutted down")
            await self._server_task
            logger.info("agent manager task completed")
            self._server_task = None
        else:
            logger.warning("no server to shutdown")

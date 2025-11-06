import logging
from typing import Optional
from pydantic import ValidationError
import uvicorn
import asyncio
import uuid
import json
from fastapi import FastAPI, WebSocket, WebSocketDisconnect
from dataclasses import dataclass

from common.models.control_protocol import Ack, AgentHello, AgentStatusUpdate, CreateSandboxRequest, CreateSandboxResponse, IncomingAdapter, Nack

logger = logging.getLogger()

@dataclass
class AgentStatus:
    last_activity: int
    # add n_active_ws from agent to count multiple ws (e.g. due to bugs or by design?)
    # should probably keep several samples / equation for trend:
    last_cpu: float
    last_mem: float
    websocket: WebSocket  # Track WebSocket connection for sending messages

class AgentManager:
    def __init__(self) -> None:
        self._active_agents: dict[uuid.UUID, AgentStatus] = {}
        self._pending_sandbox_requests: dict[str, asyncio.Future] = {}  # Track pending sandbox creation requests
        self._app = FastAPI()
        # todo benl: properly config
        config = uvicorn.Config(self._app, host="0.0.0.0", port=8765, log_level="debug")
        self._server = uvicorn.Server(config)
        self._server_task: asyncio.Task | None = None
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
                    logger.info("received: %s", msg)
                except (json.JSONDecodeError, ValidationError):
                    logger.exception("Failed to validate incoming message: %s", raw)
                    continue

                # no server-side deduplication yet

                if agent_id:
                    if isinstance(msg, AgentStatusUpdate):
                        self._active_agents[msg.agent_id].last_activity = msg.ts
                        self._active_agents[msg.agent_id].last_cpu = msg.cpu
                        self._active_agents[msg.agent_id].last_mem = msg.mem
                    elif isinstance(msg, CreateSandboxResponse):
                        # Handle sandbox creation response
                        if msg.sandbox_id in self._pending_sandbox_requests:
                            future = self._pending_sandbox_requests.pop(msg.sandbox_id)
                            future.set_result(msg)
                    elif isinstance(msg, Ack) or isinstance(msg, Nack):
                        pass
                    else:
                        logger.warning("unhandled message: %s", msg)
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
                            websocket=ws
                        )
                    else:
                        logger.warning("first message from agent is not hello, got: %s", msg)
                        # todo benl: what if message is not (n)ackable?
                        nack = Nack(nacked_id=msg.id)
                        await ws.send_text(nack.model_dump_json())
                            
        except WebSocketDisconnect:
            logger.warning("Agent disconnected: %s", agent_id)
            if agent_id and agent_id in self._active_agents:
                del self._active_agents[agent_id]
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
    
    def get_available_agent(self) -> Optional[uuid.UUID]:
        """Get the first available agent (simplest selection logic)"""
        if not self._active_agents:
            return None
        
        # For simplicty: picking the first agent for now
        return next(iter(self._active_agents.keys()))
    
    async def send_create_sandbox_request(
        self, 
        sandbox_request: CreateSandboxRequest
    ) -> Optional[CreateSandboxResponse]:
        """Send create sandbox request to an available agent"""
        agent_id = self.get_available_agent()
        if not agent_id:
            logger.warning("No available agents to handle sandbox creation")
            return None
        
        isolad_agent = self._active_agents.get(agent_id)
        if not isolad_agent or not isolad_agent.websocket:
            logger.warning(f"No WebSocket connection for agent {agent_id}")
            return None
        
        ws = isolad_agent.websocket
        
        # Create a future to wait for response
        future: asyncio.Future = asyncio.Future()
        self._pending_sandbox_requests[sandbox_request.sandbox_id] = future
        
        # Send creation request to agent
        try:
            await ws.send_text(sandbox_request.model_dump_json())
            logger.info(f"Sent create_sandbox request to agent {agent_id} for sandbox {sandbox_request.sandbox_id}")
        except Exception as e:
            logger.error(f"Failed to send request to agent {agent_id}: {e}")
            self._pending_sandbox_requests.pop(sandbox_request.sandbox_id, None)
            return None
        
        # Wait for response (with timeout)
        try:
            response = await asyncio.wait_for(future, timeout=30.0)
            return response
        except asyncio.TimeoutError:
            logger.error(f"Timeout waiting for sandbox creation response for {sandbox_request.sandbox_id}")
            self._pending_sandbox_requests.pop(sandbox_request.sandbox_id, None)
            return None

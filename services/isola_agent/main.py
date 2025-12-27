import asyncio
import logging
import sys
import uuid
import os

import uvicorn

from services.isola_agent.control_plane_client import ControlPlaneClient
from services.isola_agent.http_server import app

logger = logging.getLogger()
log_level = os.getenv("LOG_LEVEL", "info").upper()
logger.setLevel(getattr(logging, log_level, logging.INFO))

handler = logging.StreamHandler(sys.stdout)
handler.setLevel(getattr(logging, log_level, logging.INFO))

formatter = logging.Formatter(
    fmt="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S",
)
handler.setFormatter(formatter)

logger.addHandler(handler)

# Suppress verbose websockets debug logs
logging.getLogger("websockets").disabled = True
logging.getLogger("websockets.client").disabled = True
logging.getLogger("websockets.server").disabled = True
logging.getLogger("websockets.protocol").disabled = True

# HTTP server configuration
HTTP_HOST = os.getenv("HTTP_HOST", "0.0.0.0")
HTTP_PORT = int(os.getenv("HTTP_PORT", "8080"))


async def run_http_server():
    """Run the FastAPI HTTP server."""
    config = uvicorn.Config(
        app,
        host=HTTP_HOST,
        port=HTTP_PORT,
        log_level=log_level.lower(),
        access_log=True,
    )
    server = uvicorn.Server(config)
    await server.serve()


async def run_control_plane_client():
    """Run the WebSocket control plane client."""
    agent_id = uuid.uuid4()
    control_plane_url = os.getenv("ISOLA_CONTROLLER_WS_URL", "ws://isola-controller:8765/ws")
    logger.info("Starting agent %s", agent_id)
    control_plane_client = ControlPlaneClient(agent_id=agent_id, control_plane_url=control_plane_url)
    await control_plane_client.start()
    # TODO: __OMER__ verify this logic
    # start() spawns background tasks and returns immediately.
    # Wait forever so asyncio.gather() keeps both coroutines alive.
    await asyncio.Event().wait()


async def main():
    """Main entry point - runs HTTP server and control plane client concurrently."""
    logger.info("Starting Isola Agent with HTTP server on %s:%d", HTTP_HOST, HTTP_PORT)
    
    # Run both servers concurrently
    await asyncio.gather(
        run_http_server(),
        run_control_plane_client(),
    )


if __name__ == "__main__":
    asyncio.run(main())

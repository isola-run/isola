import asyncio
import logging
import sys
import os

import uvicorn

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


async def main():
    """Runs the HTTP server."""
    logger.info("Starting Isola Agent with HTTP server on %s:%d", HTTP_HOST, HTTP_PORT)
    
    await run_http_server()


if __name__ == "__main__":
    asyncio.run(main())

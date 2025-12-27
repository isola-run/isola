import logging
import os
import asyncio
from services.isola_controller.agent_manager import AgentManager
import sys

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
logger.info(f"Log level set to: {log_level}")

# Suppress verbose websockets and uvicorn debug logs
for ws_logger in [
    "websockets",
    "websockets.client", 
    "websockets.server",
    "websockets.protocol",
    "uvicorn.error",
    "uvicorn.access",
]:
    logging.getLogger(ws_logger).setLevel(logging.WARNING)

agent_manager = AgentManager()

async def main():
    logger.info("Starting Isola Control Plane...")
    
    await agent_manager.start()
    await asyncio.Event().wait()  # Block forever
    

if __name__ == "__main__":
    asyncio.run(main())

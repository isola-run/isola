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

# Suppress verbose websockets debug logs
websockets_logger = logging.getLogger("websockets")
websockets_logger.setLevel(logging.WARNING)

# Suppress uvicorn websocket protocol debug logs
# uvicorn_websockets_logger = logging.getLogger("uvicorn.protocols.websockets")
# uvicorn_websockets_logger.setLevel(logging.INFO)
# uvicorn_websockets_auto_logger = logging.getLogger("uvicorn.protocols.websockets.auto")
# uvicorn_websockets_auto_logger.setLevel(logging.INFO)
# uvicorn_protocols_logger = logging.getLogger("uvicorn.protocols")
# uvicorn_protocols_logger.setLevel(logging.INFO)

agent_manager = AgentManager()

async def main():
    logger.info("Starting Isola Control Plane...")
    
    await agent_manager.start()
    await asyncio.Event().wait()  # Block forever
    

if __name__ == "__main__":
    asyncio.run(main())

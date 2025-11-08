import asyncio
import logging
import sys
import uuid
import os
from services.isola_agent.control_plane_client import ControlPlaneClient

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


async def main():
    agent_id = uuid.uuid4()
    control_plane_url = os.getenv("ISOLA_CONTROLLER_WS_URL", "ws://isola-controller:8765/ws")
    logger.info("Starting agent %s", agent_id)
    control_plane_client = ControlPlaneClient(agent_id=agent_id, control_plane_url=control_plane_url)
    await control_plane_client.start()
    await asyncio.sleep(999)
    logger.info("Control plane client stopped")

if __name__ == "__main__":
    asyncio.run(main())
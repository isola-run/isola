import asyncio
import logging
import sys
import uuid
import os
from isola.agent.control_plane_client import ControlPlaneClient

logger = logging.getLogger()
logger.setLevel(logging.DEBUG)

handler = logging.StreamHandler(sys.stdout)
handler.setLevel(logging.DEBUG)

formatter = logging.Formatter(
    fmt="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S",
)
handler.setFormatter(formatter)

logger.addHandler(handler)


async def main():
    agent_id = uuid.uuid4()
    control_plane_url = os.getenv("ISOLA_CONTROLLER_WS_URL", "ws://controller:8765/ws")
    logger.info("Starting agent %s", agent_id)
    control_plane_client = ControlPlaneClient(agent_id=agent_id, control_plane_url=control_plane_url)
    await control_plane_client.start()
    await asyncio.sleep(999)
    logger.info("Control plane client started")

if __name__ == "__main__":
    asyncio.run(main())
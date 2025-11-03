import logging
import asyncio
from isola.control.agent_manager import AgentManager
import sys, os

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

# Create a single, reusable instance of the AgentManager
agent_manager = AgentManager()

async def main():
    print("Hello from dev-isola!")
    # Use the singleton instance
    await agent_manager.start()


if __name__ == "__main__":
    asyncio.run(main())

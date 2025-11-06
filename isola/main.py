import logging
import asyncio
from isola.control.agent_manager import AgentManager
import sys

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
    print("Starting Isola Control Plane...")
    
    # Make agent_manager available to API
    import isola.api.client_gateway as client_gateway_module
    client_gateway_module._agent_manager = agent_manager
    
    # Start the agent manager
    await agent_manager.start()
    await asyncio.sleep(999)
    print("Agent manager is running in the background.")
    print("Main application can now proceed with other tasks.")


if __name__ == "__main__":
    asyncio.run(main())

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
    print("Hello from dev-isola!")

    # Start the agent manager as a background task
    server_task = asyncio.create_task(agent_manager.start())

    print("Agent manager is running in the background.")
    print("Main application can now proceed with other tasks.")

    # --- You can add other application logic here ---
    # For example, let's just wait for a while.
    # In a real application, you might await other coroutines or events.
    try:
        # Keep the main application alive.
        # You could replace this with `server_task` to wait until the server is stopped.
        await asyncio.sleep(3600)  # Keep running for an hour
    except asyncio.CancelledError:
        print("Main task cancelled, shutting down.")
    finally:
        # Gracefully stop the agent manager when the application exits
        print("Shutting down agent manager...")
        await agent_manager.stop()
        await server_task  # Wait for the server to shut down completely


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        print("\nApplication stopped by user.")

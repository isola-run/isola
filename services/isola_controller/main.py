import logging
import os
import asyncio
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

async def main():
    logger.info("Starting Isola Control Plane...")
    # Note: This file is not currently used - the controller is started via uvicorn in the Dockerfile
    # which runs: uvicorn services.isola_controller.client_gateway:app
    await asyncio.Event().wait()  # Block forever
    

if __name__ == "__main__":
    asyncio.run(main())

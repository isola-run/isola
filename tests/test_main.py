import asyncio
from contextlib import suppress
import logging
import pytest
import websockets
import uuid

from services.isola_controller import main as main_module
from isola.models.agent_ws import AgentHello, AgentStatusUpdate

logger = logging.getLogger()


def test_hello_and_status():
    async def run_server_and_client():
        server_task = asyncio.create_task(main_module.main())
        agent_id = uuid.uuid4()
        try:
            # Wait for the server to start
            await asyncio.sleep(0.1)

            for _ in range(20):  # Retry connecting
                try:
                    async with websockets.connect("ws://localhost:8765/ws") as ws:
                        m = AgentHello(agent_id=agent_id).model_dump_json()
                        await ws.send(m)
                        ack = await ws.recv()
                        logger.info("agent hello response: %s", ack)
                        await ws.send(
                            AgentStatusUpdate(
                                agent_id=agent_id, mem=1.23, cpu=4.56
                            ).model_dump_json()
                        )
                        await ws.close()
                        logger.info("CLOSED!")
                    break
                except (OSError, ConnectionRefusedError):
                    await asyncio.sleep(0.1)
            else:
                pytest.fail("WebSocket server did not start in time")
        finally:
            server_task.cancel()
            with suppress(asyncio.CancelledError):
                await server_task

    asyncio.run(run_server_and_client())

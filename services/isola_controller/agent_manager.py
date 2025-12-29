import logging
from typing import Optional

from common.models.control_protocol import CreateSandboxRequest, CreateSandboxResponse

logger = logging.getLogger()


class AgentManager:
    """Stub AgentManager - websocket functionality removed."""
    
    def __init__(self) -> None:
        logger.warning("AgentManager initialized but websocket functionality has been removed")
    
    async def start(self) -> None:
        """Stub start method - no-op."""
        logger.info("AgentManager.start() called (no-op)")
    
    async def shutdown(self) -> None:
        """Stub shutdown method - no-op."""
        logger.info("AgentManager.shutdown() called (no-op)")
    
    def get_available_agent(self) -> Optional[str]:
        """Stub method - always returns None."""
        return None
    
    async def send_create_sandbox_request(
        self, 
        sandbox_request: CreateSandboxRequest
    ) -> Optional[CreateSandboxResponse]:
        """Stub method - always returns None (agent backend not supported without websockets)."""
        logger.warning("send_create_sandbox_request called but agent backend is not available (websockets removed)")
        return None

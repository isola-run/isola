from typing import Optional

from isola.api.models.common import Labels
from isola.api.models.sandbox import (
    CreateSandbox,
    Sandbox,
    SandboxList,
    SandboxState,
)

class SandboxImpl:
    def __init__(self, store):
        self.store = store

    async def list_sandboxes(self, state: Optional[SandboxState], limit: int, offset: int) -> SandboxList:
        return self.store.list(state, limit, offset)

    async def create_sandbox(self, req: CreateSandbox) -> Sandbox:
        return self.store.create(req)

    async def get_sandbox(self, sandbox_id: str) -> Sandbox:
        return self.store.get(sandbox_id)

    async def delete_sandbox(self, sandbox_id: str, force: bool) -> None:
        self.store.delete(sandbox_id, force)

    async def start_sandbox(self, sandbox_id: str) -> Sandbox:
        return self.store.start(sandbox_id)

    async def stop_sandbox(self, sandbox_id: str) -> Sandbox:
        return self.store.stop(sandbox_id)

    async def restart_sandbox(self, sandbox_id: str) -> Sandbox:
        return self.store.restart(sandbox_id)

    async def update_sandbox_labels(self, sandbox_id: str, labels: Labels) -> Sandbox:
        return self.store.update_labels(sandbox_id, labels)
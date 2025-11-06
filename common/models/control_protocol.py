import time
import uuid
from typing import Annotated, Literal, Union, Dict, Optional

from pydantic import BaseModel, Field, TypeAdapter


class MsgBase(BaseModel):
    v: Literal[1] = 1  # schema version
    ts: int = Field(default_factory=lambda: int(time.time() * 1000))
    id: uuid.UUID = Field(default_factory=lambda: uuid.uuid4())


class Ack(MsgBase):
    type: Literal["ack"] = "ack"
    acked_id: uuid.UUID


class Nack(MsgBase):
    type: Literal["nack"] = "nack"
    nacked_id: uuid.UUID

class CreateSandboxRequest(MsgBase):
    type: Literal["create_sandbox"] = "create_sandbox"
    sandbox_id: str
    name: str
    image: str
    cpu: float
    memory: float
    disk: float
    env: Dict[str, str]
    labels: Dict[str, str]

# agent -> manager
class AgentHello(MsgBase):
    type: Literal["hello"] = "hello"
    agent_id: uuid.UUID


class AgentStatusUpdate(MsgBase):
    type: Literal["status"] = "status"
    agent_id: uuid.UUID
    cpu: float
    mem: float


class CreateSandboxResponse(MsgBase):
    type: Literal["sandbox_created"] = "sandbox_created"
    sandbox_id: str
    success: bool
    error_reason: Optional[str] = None
    ip_address: Optional[str] = None

Incoming = Annotated[
    Union[AgentHello, AgentStatusUpdate, CreateSandboxResponse],
    Field(discriminator="type"),
]
IncomingAdapter: TypeAdapter[Incoming] = TypeAdapter(Incoming)


# manager -> agent
Outgoing = Annotated[
    Union[Ack, Nack, CreateSandboxRequest],
    Field(discriminator="type"),
]


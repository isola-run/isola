import time
import uuid
from typing import Annotated, Literal, Union

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


# agent -> manager
class AgentHello(MsgBase):
    type: Literal["hello"] = "hello"
    agent_id: uuid.UUID


class AgentStatusUpdate(MsgBase):
    type: Literal["status"] = "status"
    agent_id: uuid.UUID
    cpu: float
    mem: float


Incoming = Annotated[
    Union[AgentHello, AgentStatusUpdate],
    Field(discriminator="type"),
]
IncomingAdapter: TypeAdapter[Incoming] = TypeAdapter(Incoming)


# manager -> agent
Outgoing = Annotated[
    Union[Ack, Nack],
    Field(discriminator="type"),
]


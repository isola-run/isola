from __future__ import annotations

import pytest

from isola import (
    APIConnectionError,
    APIError,
    BadGatewayError,
    BadRequestError,
    InternalError,
    IsolaError,
    NotFoundError,
)
from isola._exceptions import is_transient


@pytest.mark.parametrize(
    ("exc", "expected"),
    [
        (APIConnectionError("conn refused"), True),
        (BadGatewayError(status_code=502, message="bad gateway"), True),
        (APIError(status_code=503, message="unavailable"), True),
        (APIError(status_code=504, message="timeout"), True),
        (BadRequestError(status_code=400, message="bad"), False),
        (NotFoundError(status_code=404, message="not found"), False),
        (InternalError(status_code=500, message="internal"), False),
        (IsolaError("generic"), False),
    ],
    ids=[
        "APIConnectionError",
        "BadGatewayError-502",
        "APIError-503",
        "APIError-504",
        "BadRequestError-400",
        "NotFoundError-404",
        "InternalError-500",
        "IsolaError-generic",
    ],
)
def test_is_transient(exc: IsolaError, expected: bool) -> None:
    assert is_transient(exc) is expected

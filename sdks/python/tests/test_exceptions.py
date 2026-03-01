# Copyright The Isola Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

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

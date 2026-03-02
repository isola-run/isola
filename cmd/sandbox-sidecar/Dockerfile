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

FROM golang:1.26.0-bookworm AS builder
ARG TARGETARCH

WORKDIR /workspace

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy required source
COPY cmd/sandbox-sidecar/ cmd/sandbox-sidecar/
COPY internal/ internal/

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -ldflags="-w -s" -o sandbox-sidecar ./cmd/sandbox-sidecar/main.go

FROM gcr.io/distroless/static

WORKDIR /
COPY --from=builder /workspace/sandbox-sidecar .

EXPOSE 10032

ENTRYPOINT ["/sandbox-sidecar"]

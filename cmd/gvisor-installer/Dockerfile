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

FROM --platform=$BUILDPLATFORM golang:1.26.5-bookworm AS builder

ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_DATE=unknown
ARG GIT_TREE_STATE=unknown
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY cmd/gvisor-installer/ cmd/gvisor-installer/
COPY internal/ internal/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
      -ldflags="-w -s \
        -X github.com/isola-run/isola/internal/version.gitVersion=${VERSION} \
        -X github.com/isola-run/isola/internal/version.gitCommit=${GIT_COMMIT} \
        -X github.com/isola-run/isola/internal/version.buildDate=${BUILD_DATE} \
        -X github.com/isola-run/isola/internal/version.gitTreeState=${GIT_TREE_STATE}" \
      -o gvisor-installer ./cmd/gvisor-installer/main.go

# Alpine (not distroless): the installer needs nsenter to run systemctl and
# containerd in the host's mount namespace, and runs as root by design
# (privileged DaemonSet that writes host paths).
FROM alpine:3.21

RUN apk add --no-cache util-linux-misc

COPY --from=builder /workspace/gvisor-installer /gvisor-installer

ENTRYPOINT ["/gvisor-installer"]

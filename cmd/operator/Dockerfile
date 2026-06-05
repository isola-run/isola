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

FROM --platform=$BUILDPLATFORM golang:1.26.4-bookworm AS builder

ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_DATE=unknown
ARG GIT_TREE_STATE=unknown
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# Copy required source
COPY api/ api/
COPY cmd/operator/ cmd/operator/
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
      -o operator ./cmd/operator/main.go

FROM gcr.io/distroless/static-debian13:nonroot

COPY --from=builder /workspace/operator /operator
USER 65532:65532
ENTRYPOINT ["/operator"]

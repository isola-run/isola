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

# Build nsenter statically from source (only nsenter, nothing else)
FROM debian:bookworm-slim AS nsenter-builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    git ca-certificates gnupg \
    autoconf automake autopoint libtool pkg-config gettext \
    gcc make bison flex \
    && rm -rf /var/lib/apt/lists/*

# Clone from canonical kernel.org source and verify the GPG-signed release tag.
# All util-linux tags are signed by maintainer Karel Zak <kzak@redhat.com> (https://kzak.redcrew.org/).
# Key fingerprint: B0C64D14301CC6EFAEDF60E4E4B71D5EEC39C284
# Ref: https://git.kernel.org/pub/scm/docs/kernel/pgpkeys.git/tree/keys/E4B71D5EEC39C284.asc
ARG UTIL_LINUX_VERSION=v2.41.3
RUN git clone --branch $UTIL_LINUX_VERSION https://git.kernel.org/pub/scm/utils/util-linux/util-linux.git /util-linux
WORKDIR /util-linux
RUN gpg --keyserver keyserver.ubuntu.com --recv-keys B0C64D14301CC6EFAEDF60E4E4B71D5EEC39C284 \
    && git verify-tag $UTIL_LINUX_VERSION

RUN ./autogen.sh \
    && ./configure \
        --disable-all-programs \
        --enable-nsenter \
        --enable-static-programs=nsenter \
        --disable-nls \
    && make -j$(nproc)

# Sidecar runs as root (set by operator SecurityContext) to access /proc/<pid>/environ
# and /proc/<pid>/root of sandbox containers via shared PID namespace.
FROM gcr.io/distroless/static

WORKDIR /
COPY --from=nsenter-builder /util-linux/nsenter.static /usr/bin/nsenter
COPY --from=builder /workspace/sandbox-sidecar .
COPY LICENSE /LICENSE

EXPOSE 10032

ENTRYPOINT ["/sandbox-sidecar"]

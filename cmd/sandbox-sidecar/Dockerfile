FROM golang:1.25.7-bookworm AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy required source
COPY cmd/sandbox-sidecar/ cmd/sandbox-sidecar/
COPY internal/ internal/

# Build
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags="-w -s" -o sandbox-sidecar ./cmd/sandbox-sidecar/main.go

# TODO benl: not all nsenter deps seem to be copied (ldd nsenter)
# Install nsenter for entering container namespaces during command execution
FROM debian:bookworm-slim AS tools
RUN apt-get update && apt-get install -y --no-install-recommends util-linux \
    && rm -rf /var/lib/apt/lists/*

# Sidecar runs as root (set by operator SecurityContext) to access /proc/<pid>/environ
# and /proc/<pid>/root of sandbox containers via shared PID namespace.
# Uses distroless/base (not /static) because nsenter requires glibc.
FROM gcr.io/distroless/base

WORKDIR /
COPY --from=tools /usr/bin/nsenter /usr/bin/nsenter
COPY --from=builder /workspace/sandbox-sidecar .

EXPOSE 10032

ENTRYPOINT ["/sandbox-sidecar"]

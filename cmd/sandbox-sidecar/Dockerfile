FROM golang:1.25.7-bookworm@sha256:38342f3e7a504bf1efad858c18e771f84b66dc0b363add7a57c9a0bbb6cf7b12 AS builder
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

# Use distroless as minimal base image
# Sidecar runs as root (set by operator SecurityContext) to access /proc/<pid>/environ
# and /proc/<pid>/root of sandbox containers via shared PID namespace
FROM gcr.io/distroless/static

WORKDIR /
COPY --from=builder /workspace/sandbox-sidecar .

EXPOSE 10032

ENTRYPOINT ["/sandbox-sidecar"]

FROM golang:1.25.5-bookworm@sha256:2c7c65601b020ee79db4c1a32ebee0bf3d6b298969ec683e24fcbea29305f10e AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy required source
COPY api/ api/
COPY cmd/api-gateway/ cmd/api-gateway/
COPY internal/api-gateway/ internal/api-gateway/
COPY internal/logging/ internal/logging/

# Build
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags="-w -s" -o api-gateway ./cmd/api-gateway/main.go

# Use distroless as minimal base image
FROM gcr.io/distroless/static:nonroot

WORKDIR /
COPY --from=builder /workspace/api-gateway .
USER 65532:65532

EXPOSE 8080

ENTRYPOINT ["/api-gateway"]

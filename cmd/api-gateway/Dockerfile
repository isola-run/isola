FROM golang:1.25.7-bookworm AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy required source
COPY api/ api/
COPY cmd/api-gateway/ cmd/api-gateway/
COPY internal/ internal/

# Build
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags="-w -s" -o api-gateway ./cmd/api-gateway/main.go

# Use distroless as minimal base image
FROM gcr.io/distroless/static:nonroot

WORKDIR /
COPY --from=builder /workspace/api-gateway .
USER 65532:65532

EXPOSE 8080

ENTRYPOINT ["/api-gateway"]

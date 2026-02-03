FROM golang:1.25.5-bookworm@sha256:2c7c65601b020ee79db4c1a32ebee0bf3d6b298969ec683e24fcbea29305f10e AS builder

WORKDIR /workspace

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy required source
COPY cmd/uploader/ cmd/uploader/
COPY internal/ internal/

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o uploader ./cmd/uploader/main.go

# Use distroless as minimal base image
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /workspace/uploader /uploader

USER 65532:65532

ENTRYPOINT ["/uploader"]

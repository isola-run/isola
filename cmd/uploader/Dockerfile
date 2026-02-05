FROM golang:1.25.7-bookworm@sha256:38342f3e7a504bf1efad858c18e771f84b66dc0b363add7a57c9a0bbb6cf7b12 AS builder

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

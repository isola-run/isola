# Build stage
FROM golang:1.25.5-bookworm AS builder

WORKDIR /workspace

# Copy go mod files first for caching
COPY go.mod go.sum* ./
RUN go mod download

# Copy source
COPY . .

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o uploader ./cmd/main.go

# Final stage - minimal image
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /workspace/uploader /uploader

USER 65532:65532

ENTRYPOINT ["/uploader"]

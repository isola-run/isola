# Build the isola-gw binary
FROM golang:1.25 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# Cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the Go source
COPY cmd/ cmd/
COPY internal/ internal/

# Build
# CGO_ENABLED=0 produces a statically linked binary
# The GOARCH has no default value to allow the binary to be built according to the host where the command
# was called. For example, if we call docker build on Apple Silicon M1, the docker BUILDPLATFORM arg will be
# linux/arm64, when for Apple x86 it will be linux/amd64.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -ldflags="-w -s" -o isola-gw ./cmd/main.go

# Use distroless as minimal base image to package the binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot

WORKDIR /
COPY --from=builder /workspace/isola-gw .
USER 65532:65532

# Expose the HTTP port
EXPOSE 8080

ENTRYPOINT ["/isola-gw"]


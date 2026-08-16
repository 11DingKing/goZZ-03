# syntax=docker/dockerfile:1

# Build stage: pin to the Go 1.26 line (no "latest"). $BUILDPLATFORM lets buildx
# cross-compile for both amd64 and arm64 from a single invocation.
FROM --platform=$BUILDPLATFORM golang:1.26 AS builder
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/arcticdispatch ./cmd/server

# Runtime stage: distroless static keeps only the binary. Non-root, multi-arch.
FROM --platform=$TARGETPLATFORM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/arcticdispatch /arcticdispatch
EXPOSE 59041
ENTRYPOINT ["/arcticdispatch"]

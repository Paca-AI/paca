# syntax=docker/dockerfile:1

# ── Build stage ──────────────────────────────────────────────────────────────
# --platform=$BUILDPLATFORM pins this stage to the host arch (e.g. amd64)
# regardless of TARGETPLATFORM, so multi-arch builds cross-compile natively
# via GOOS/GOARCH instead of running the Go compiler under QEMU emulation for
# arm64 — that emulation was the dominant cost in multi-platform CI builds.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH

# GOMAXPROCS/GOMEMLIMIT/-p default to single-core/low-memory for small Docker
# Desktop allocations (local builds, e2e CI). CD overrides these via
# --build-arg since GitHub Actions runners have more headroom and native
# cross-compilation removes the memory pressure that motivated the low
# defaults in the first place.
ARG GOMAXPROCS=1
ARG GOMEMLIMIT=768MiB
ARG GOBUILD_P=1
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOMAXPROCS=$GOMAXPROCS GOMEMLIMIT=$GOMEMLIMIT \
	go build -p=$GOBUILD_P -ldflags="-s -w" -o api ./cmd/api

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.21

# Release tag this image was built from (e.g. "v1.2.3"), baked in by CD via
# --build-arg. Read at startup as PACA_VERSION; defaults to "dev" for local
# builds. See internal/config/load.go and internal/transport/http/handler/version_handler.go.
ARG PACA_VERSION=dev
ENV PACA_VERSION=$PACA_VERSION

RUN apk add --no-cache ca-certificates tzdata su-exec

# Create a dedicated non-root user and group for running the API
RUN addgroup -S app && adduser -S app -G app

WORKDIR /app

COPY --from=builder /build/api .

# Copy entrypoint script
COPY docker-entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Ensure the binary is owned by the non-root user
RUN chown app:app /app/api

EXPOSE 8080

# Use entrypoint script (runs as root, then switches to app user)
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["./api"]

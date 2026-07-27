# syntax=docker/dockerfile:1

# ── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Keep compile-time memory usage low for small Docker Desktop allocations.
RUN CGO_ENABLED=0 GOOS=linux GOMAXPROCS=1 GOMEMLIMIT=768MiB \
	go build -p=1 -ldflags="-s -w" -o api ./cmd/api

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

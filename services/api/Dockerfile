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

RUN apk add --no-cache ca-certificates tzdata

# Create a dedicated non-root user and group for running the API
RUN addgroup -S app && adduser -S app -G app

WORKDIR /app

COPY --from=builder /build/api .

# Ensure the binary is owned by the non-root user
RUN chown app:app /app/api

EXPOSE 8080

# Run the API as the non-root user
USER app:app
CMD ["./api"]

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

WORKDIR /app

COPY --from=builder /build/api .

EXPOSE 8080

CMD ["./api"]

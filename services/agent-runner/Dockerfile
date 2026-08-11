# syntax=docker/dockerfile:1

# ── Build stage ──────────────────────────────────────────────────────────────
# --platform=$BUILDPLATFORM: see services/api/Dockerfile's identical comment —
# cross-compiles via GOOS/GOARCH instead of running the Go compiler under
# QEMU emulation for a non-native TARGETPLATFORM.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG GOMAXPROCS=1
ARG GOMEMLIMIT=768MiB
ARG GOBUILD_P=1
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOMAXPROCS=$GOMAXPROCS GOMEMLIMIT=$GOMEMLIMIT \
	go build -p=$GOBUILD_P -ldflags="-s -w" -o agent-runner ./cmd/agent-runner

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /build/agent-runner .
# data/llm_models.json — served at GET /llm/models (internal/acpbridge.Server,
# LLMModelsPath default "./data/llm_models.json", relative to this WORKDIR).
# Missed on the first pass of this Dockerfile: only the binary was copied,
# so /llm/models would have 404'd in any real deployment. Caught wiring up
# the dev compose entry, not by any test — this directory has no other
# reason to be exercised by anything that runs in CI.
COPY --from=builder /build/data ./data

# Runs as root, unlike services/api's runtime image — this process needs
# read/write access to the mounted Docker socket (/var/run/docker.sock) to
# spawn/stop sandbox containers, and there's no non-root group-membership
# dance that reliably grants that across host Docker configurations. Same
# choice services/ai-agent's own Dockerfile already makes for the identical
# need; see docs/ai-agent/goose-migration.md.
CMD ["./agent-runner"]

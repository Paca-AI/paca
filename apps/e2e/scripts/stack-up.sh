#!/usr/bin/env bash
# Boots the dedicated E2E docker stack (deploy/docker-compose.e2e.yml).
#
# The agent-runner service spawns per-conversation sandbox containers from
# a separate image (paca-agent-server-goose:e2e) that docker-compose.e2e.yml
# does not build itself — see the AGENT_SERVER_IMAGE comment in that file.
# This script builds that image first, then brings up the rest of the stack.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
COMPOSE_FILE="$ROOT_DIR/deploy/docker-compose.e2e.yml"

echo "==> Building paca-agent-server-goose:e2e"
docker build \
  -f "$ROOT_DIR/services/agent-server/Dockerfile" \
  -t paca-agent-server-goose:e2e \
  "$ROOT_DIR/services/agent-server"

echo "==> Starting E2E stack"
docker compose -f "$COMPOSE_FILE" up -d --build --wait

echo "==> Stack is up. Base URL: http://localhost"

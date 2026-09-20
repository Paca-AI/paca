#!/usr/bin/env bash
# Tears down the dedicated E2E docker stack (deploy/docker-compose.e2e.yml),
# including its named volumes (fresh database/state on next stack-up.sh run).
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
COMPOSE_FILE="$ROOT_DIR/deploy/docker-compose.e2e.yml"

docker compose -f "$COMPOSE_FILE" down -v

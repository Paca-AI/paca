#!/usr/bin/env bash
# Install/update the com.galaxy.sdd plugin on the PROD galaxy-paca stack.
#
# Prod uses named volumes (deploy/docker-compose.prod.yml):
#   backend_plugins  -> api:/plugins            (backend.wasm + plugin.json)
#   frontend_plugins -> api:/plugins-frontend   (same volume the gateway
#                        serves read-only at /plugins/* from /var/www/plugins)
# so unlike dev (bind mounts under plugins/local/), files are copied INTO the
# volumes through the running api container, then the plugin is registered
# via the admin API (which loads the stub WASM immediately — copy first!).
#
# Usage (on the prod host, after ./build.sh):
#   API_KEY=<paca-api-key> ./install-prod.sh
#
# Env overrides:
#   API_URL        (default https://tasks.skyplatform.net)
#   API_CONTAINER  (default galaxy-paca-api-1)
#   API_KEY        (required — Paca web: Settings -> API Keys)
set -euo pipefail
cd "$(dirname "$0")"

PLUGIN_ID=$(jq -r '.id' plugin.json)
PLUGIN_VERSION=$(jq -r '.version' plugin.json)
API_URL="${API_URL:-https://tasks.skyplatform.net}"
API_CONTAINER="${API_CONTAINER:-galaxy-paca-api-1}"
: "${API_KEY:?API_KEY is required (create one in Paca: Settings -> API Keys)}"

fail() { echo "FAIL: $*" >&2; exit 1; }
ok()   { echo "ok   $*"; }

command -v jq >/dev/null 2>&1 || fail "jq is required"
command -v docker >/dev/null 2>&1 || fail "docker is required"
[[ -f frontend/dist/assets/remoteEntry.js ]] || fail "frontend/dist/assets/remoteEntry.js missing — run ./build.sh first"
[[ -f backend/backend.wasm ]] || fail "backend/backend.wasm missing — run ./build.sh first"
docker inspect "$API_CONTAINER" >/dev/null 2>&1 || fail "container $API_CONTAINER not found (override with API_CONTAINER=...)"

# ── 1. Stage the two store layouts ───────────────────────────────────────────
STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT
mkdir -p "$STAGE/backend/$PLUGIN_ID/migrations" "$STAGE/frontend/$PLUGIN_ID"
cp backend/backend.wasm "$STAGE/backend/$PLUGIN_ID/backend.wasm"
cp plugin.json          "$STAGE/backend/$PLUGIN_ID/plugin.json"
cp -R frontend/dist/.   "$STAGE/frontend/$PLUGIN_ID/"
rm -f "$STAGE/frontend/$PLUGIN_ID/index.html" "$STAGE/frontend/$PLUGIN_ID/assets/remoteEntry.ssr.js"

# ── 2. Copy into the volumes via the api container ───────────────────────────
# Best-effort cleanup so stale hashed chunks from previous builds don't pile up
# (they are unreferenced, so failure here is non-fatal).
docker exec -u 0 "$API_CONTAINER" rm -rf "/plugins-frontend/$PLUGIN_ID" 2>/dev/null \
  || echo "warn: could not clean /plugins-frontend/$PLUGIN_ID (continuing)"
docker cp "$STAGE/backend/$PLUGIN_ID"  "$API_CONTAINER:/plugins/"
docker cp "$STAGE/frontend/$PLUGIN_ID" "$API_CONTAINER:/plugins-frontend/"
ok "stores populated: /plugins/$PLUGIN_ID + /plugins-frontend/$PLUGIN_ID"

# ── 3. Register (or update) via the admin API ────────────────────────────────
LIST=$(curl -fsS -H "X-API-Key: $API_KEY" "$API_URL/api/v1/plugins") \
  || fail "GET $API_URL/api/v1/plugins failed (API key? gateway?)"
UUID=$(echo "$LIST" | jq -r --arg n "$PLUGIN_ID" '.data.plugins[] | select(.name == $n) | .id' | head -n1)
MANIFEST=$(cat plugin.json)

if [[ -n "$UUID" && "$UUID" != "null" ]]; then
  echo "plugin already registered ($UUID) — updating to $PLUGIN_VERSION"
  HTTP=$(curl -sS -o /tmp/sdd-plugin-resp.$$ -w '%{http_code}' -X PATCH \
    "$API_URL/api/v1/admin/plugins/$UUID" \
    -H "X-API-Key: $API_KEY" -H "Content-Type: application/json" \
    -d "{\"version\":\"$PLUGIN_VERSION\",\"manifest\":$MANIFEST,\"enabled\":true}")
  [[ "$HTTP" == "200" ]] || { cat /tmp/sdd-plugin-resp.$$; rm -f /tmp/sdd-plugin-resp.$$; fail "update failed (HTTP $HTTP)"; }
else
  echo "registering $PLUGIN_ID $PLUGIN_VERSION"
  HTTP=$(curl -sS -o /tmp/sdd-plugin-resp.$$ -w '%{http_code}' -X POST \
    "$API_URL/api/v1/admin/plugins" \
    -H "X-API-Key: $API_KEY" -H "Content-Type: application/json" \
    -d "{\"name\":\"$PLUGIN_ID\",\"version\":\"$PLUGIN_VERSION\",\"manifest\":$MANIFEST,\"enabled\":true}")
  [[ "$HTTP" == "201" ]] || { cat /tmp/sdd-plugin-resp.$$; rm -f /tmp/sdd-plugin-resp.$$; fail "install failed (HTTP $HTTP)"; }
fi
rm -f /tmp/sdd-plugin-resp.$$
ok "plugin registered and enabled"

# ── 4. Verify ────────────────────────────────────────────────────────────────
ENTRY_HEAD=$(curl -fsS "$API_URL/plugins/$PLUGIN_ID/assets/remoteEntry.js" | head -c 6) \
  || fail "gateway does not serve /plugins/$PLUGIN_ID/assets/remoteEntry.js"
[[ "$ENTRY_HEAD" == "import" ]] || fail "remoteEntry.js served but does not look like an ES module"
ok "gateway serves $API_URL/plugins/$PLUGIN_ID/assets/remoteEntry.js"
curl -fsS -H "X-API-Key: $API_KEY" "$API_URL/api/v1/plugins" \
  | jq -r --arg n "$PLUGIN_ID" '.data.plugins[] | select(.name == $n) | "ok   registry: \(.name) \(.version) enabled=\(.enabled)"'

cat <<EOF

Done. In the SPA (allow ~5 min for the plugins query cache, or hard-reload):
  - project sidebar shows the "SDD Sensor" card and the "SDD Fleet" nav item
  - /projects/<id>/plugins/$PLUGIN_ID/sdd-fleet renders the embedded sensor
  - "+ Add view" on any board offers the SDD Fleet plugin layout
If the frame stays blank: the sensor must allow being framed by
https://tasks.skyplatform.net (unset X-Frame-Options / set CSP frame-ancestors
on the sensor deployment) — see README.md.
EOF

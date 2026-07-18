#!/usr/bin/env bash
# Build the com.galaxy.sdd plugin (ADR-038 T6):
#   1. validate plugin.json against the host's real constraints
#      (no formal JSON schema exists in-repo; the authoritative rules live in
#      services/api/internal/domain/plugin/entity.go and
#      apps/web/src/lib/plugin-api.ts — the checks below mirror them),
#   2. build the frontend Module Federation remote  -> frontend/dist/assets/remoteEntry.js
#   3. build the inert stub backend                 -> backend/backend.wasm
#      (Go wasip1 c-shared when Go is available, else an 8-byte empty module —
#       both verified to load under wazero exactly like Runtime.Load does).
set -euo pipefail
cd "$(dirname "$0")"

fail() { echo "FAIL: $*" >&2; exit 1; }
ok()   { echo "ok   $*"; }

command -v jq >/dev/null 2>&1 || fail "jq is required"

# ── 1. Manifest validation ───────────────────────────────────────────────────
M=plugin.json
jq -e . "$M" >/dev/null || fail "$M is not valid JSON"

ID=$(jq -r '.id' "$M")
[[ "$ID" == "com.galaxy.sdd" ]] || fail "id must be com.galaxy.sdd (got: $ID)"
[[ "$ID" =~ ^[a-z0-9-]+(\.[a-z0-9-]+)+$ ]] || fail "id must be reverse-DNS"
jq -e '.displayName | strings | length > 0' "$M" >/dev/null || fail "displayName required"
jq -r '.version' "$M" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || fail "version must be semver"

# Backend must be a NON-NULL object even for a frontend-only plugin:
# Runtime.EmitEvent walks Manifest.Backend.EventSubscriptions without a nil
# check, so omitting "backend" would nil-panic the API once the plugin loads.
jq -e '.backend | type == "object"' "$M" >/dev/null \
  || fail '"backend" must be an object ({} at minimum) — see backend/main.go'
jq -e '(.backend.routes // []) == [] and (.backend.eventSubscriptions // []) == []' "$M" >/dev/null \
  || fail "stub backend must not declare routes/eventSubscriptions"

# Allow an optional ?v=N cache-bust query (CF edge caches assets ~4h, so the
# manifest URL is versioned to force a fresh fetch — see README "Cache busting").
jq -e --arg u "/plugins/$ID/assets/remoteEntry.js" \
  '.frontend.remoteEntryUrl == $u or (.frontend.remoteEntryUrl | startswith($u + "?"))' "$M" >/dev/null \
  || fail "frontend.remoteEntryUrl must be /plugins/$ID/assets/remoteEntry.js[?v=N]"

# Extension points known to the host (apps/web/src/lib/plugin-api.ts ExtensionPointId).
jq -e '[.frontend.extensionPoints[].point] - ["sidebar.general.section","sidebar.project.section","task.detail.section","project.settings.tab","view","project.page","admin.page"] == []' "$M" >/dev/null \
  || fail "unknown extension point in frontend.extensionPoints"
jq -e '[.frontend.extensionPoints[].component] | all(length > 0)' "$M" >/dev/null \
  || fail "every extension point needs a component"

# Every navItem must reference a component registered at project.page/admin.page
# for its scope (buildNavItems silently drops mismatches).
jq -e '
  (.frontend.extensionPoints) as $eps
  | [ .frontend.navItems[]?
      | . as $n
      | ($eps | map(select(.point == (if $n.scope == "admin" then "admin.page" else "project.page" end)
                           and .component == $n.component)) | length) > 0
        and ($n.scope == "project" or $n.scope == "admin")
        and (($n.slug | length) > 0)
    ] | all' "$M" >/dev/null \
  || fail "navItems must use scope project|admin, a slug, and a component registered at project.page/admin.page"

# customPermissions (none in v1) must stay namespaced "sdd." (entity.go Validate()).
jq -e '[.customPermissions[]?.key | startswith("sdd.")] | all' "$M" >/dev/null \
  || fail 'customPermissions keys must be namespaced "sdd."'

# Exposed components must exist in the vite federation config.
for comp in $(jq -r '.frontend.extensionPoints[].component' "$M" | sort -u); do
  grep -q "\"./$comp\"" frontend/vite.config.ts || fail "component $comp not exposed in frontend/vite.config.ts"
done
ok "$M validates (mirrors entity.go + plugin-api.ts rules)"

# ── 2. Frontend remote ───────────────────────────────────────────────────────
cd frontend
if command -v bun >/dev/null 2>&1; then
  bun install
  bun run build
  bun run smoke
else
  npm install
  npm run build
  npm run smoke
fi
[[ -f dist/assets/remoteEntry.js ]] || fail "frontend build did not produce dist/assets/remoteEntry.js"
ok "frontend: dist/assets/remoteEntry.js ($(wc -c < dist/assets/remoteEntry.js | tr -d ' ') bytes)"
cd ..

# ── 3. Stub backend ──────────────────────────────────────────────────────────
cd backend
if command -v go >/dev/null 2>&1; then
  GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o backend.wasm .
  ok "backend: stub backend.wasm built with Go ($(wc -c < backend.wasm | tr -d ' ') bytes)"
else
  # Empty WASM module ("\0asm" + version 1). wazero compiles it, skips the
  # missing _initialize start function, and the runtime nil-guards every other
  # export — verified against wazero v1.11.0. See main.go for the full story.
  printf '\x00\x61\x73\x6d\x01\x00\x00\x00' > backend.wasm
  ok "backend: Go not found — wrote 8-byte empty-module stub backend.wasm"
fi
cd ..

echo ""
echo "Build complete. Next:"
echo "  dev  (repo root): ./scripts/install-local-plugin.sh deploy/galaxy/plugins/com.galaxy.sdd --api-key <key>"
echo "  prod (this dir) : API_KEY=<key> ./install-prod.sh   (see README.md)"

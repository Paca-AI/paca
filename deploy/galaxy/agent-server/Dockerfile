# =============================================================================
# Galaxy agent-server image (ADR-038): upstream sandbox + the FORK's Paca MCP
# =============================================================================
#
# The upstream ghcr.io/paca-ai/paca-agent-server image bundles the PUBLISHED
# @paca-ai/paca-mcp npm package as a global install:
#
#   /usr/local/lib/node_modules/@paca-ai/paca-mcp/{build,node_modules,package.json}
#   /usr/local/bin/paca -> …/@paca-ai/paca-mcp/build/index.js
#
# (verified by inspecting 0.9.7 on the prod host). The ai-agent service
# launches it inside each sandbox via `npx -y @paca-ai/paca-mcp`, which
# resolves to that global install — no registry fetch at runtime.
#
# This Dockerfile overlays the FORK's apps/mcp build (PACA_AUTH_MODE=bearer:
# Authorization: Bearer platform tokens instead of X-API-Key/X-Agent-ID
# header impersonation) over the published package, replacing BOTH build/
# and node_modules/ so the bundle always runs against its own dependency
# pins rather than whatever the published version shipped.
#
# Build ON THE PROD HOST from the repo root (context = apps/mcp, which is
# clean there — node_modules/build are gitignored):
#
#   docker build \
#     -f deploy/galaxy/agent-server/Dockerfile \
#     -t galaxy-local/paca-agent-server:0.9.7-galaxy.1 \
#     apps/mcp
#
# then pin it in deploy/galaxy/.env.galaxy:
#
#   AGENT_SERVER_IMAGE=galaxy-local/paca-agent-server:0.9.7-galaxy.1
#
# Bump the -galaxy.N suffix on every rebuild — sandboxes are created from
# this tag by the Docker daemon, so a stale same-tag image would silently
# keep serving the old bundle.

# ── Stage 1: dependency install (bun, frozen lockfile — apps/mcp has no
#    package-lock.json, its lockfile is bun.lock) ─────────────────────────────
FROM oven/bun:1 AS deps
WORKDIR /mcp
COPY package.json bun.lock ./
# --ignore-scripts: the package's prepare script shells out to npm/node,
# which the bun image does not provide — the build stage compiles explicitly.
RUN bun install --frozen-lockfile --ignore-scripts

# ── Stage 2: TypeScript build (node — the build script shells out to
#    `node -e …`, which the bun image does not provide) ──────────────────────
FROM node:22-bookworm-slim AS build
WORKDIR /mcp
COPY --from=deps /mcp/node_modules ./node_modules
COPY package.json tsconfig.json ./
COPY src ./src
RUN npm run build

# ── Stage 3: production-only dependency tree for the runtime overlay ─────────
FROM oven/bun:1 AS proddeps
WORKDIR /mcp
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile --production --ignore-scripts

# ── Final: upstream agent-server + fork MCP overlay ──────────────────────────
FROM ghcr.io/paca-ai/paca-agent-server:0.9.7

# The upstream image runs as USER openhands, which cannot modify the global
# npm tree — switch to root for the overlay and back afterwards.
USER root

# Replace the published package's code and dependencies with the fork's.
# The package.json (with its bin -> build/index.js mapping that the
# /usr/local/bin/paca symlink and npx resolution rely on) stays in place.
RUN rm -rf /usr/local/lib/node_modules/@paca-ai/paca-mcp/build \
           /usr/local/lib/node_modules/@paca-ai/paca-mcp/node_modules
COPY --from=build    /mcp/build        /usr/local/lib/node_modules/@paca-ai/paca-mcp/build
COPY --from=proddeps /mcp/node_modules /usr/local/lib/node_modules/@paca-ai/paca-mcp/node_modules

# Provenance marker for `docker exec … cat` sanity checks; keep the tree
# world-readable for the runtime user.
RUN printf 'galaxy-fork paca-mcp overlay (PACA_AUTH_MODE=bearer, ADR-038)\n' \
      > /usr/local/lib/node_modules/@paca-ai/paca-mcp/GALAXY_OVERLAY \
  && chmod -R a+rX /usr/local/lib/node_modules/@paca-ai/paca-mcp

USER openhands

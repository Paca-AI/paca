# Sandbox image for `llm`-type agent conversations under
# services/agent-runner (see docs/ai-agent/goose-migration.md) — the Goose-
# based replacement for this directory's old OpenHands-based Dockerfile
# (same filename, different content — removed along with services/ai-agent
# itself; see the migration doc's "Full Removal of services/ai-agent"
# section). This is now the only sandbox image in this directory and the
# one every deployment surface (dev/prod/e2e compose, CD) actually builds
# and publishes.
#
# Pinned by DIGEST, not a version tag — ghcr.io/block/goose has no current
# tagged release image to pin to instead. Verified directly (see the
# migration doc's "no current pinned Goose image tag" open risk): the
# registry only publishes floating `main`/`main-<shortsha>` CI builds plus
# one stale `1.9.0` release, despite GitHub releases going up to v1.45.0 at
# the time this was written. A digest is fully immutable regardless of what
# upstream does to `main` afterward, which solves the actual reproducibility
# risk — but note it does NOT correspond to any particular git tag or
# changelog entry the way the old OpenHands-based Dockerfile's pin did; it's
# whatever commit `main` happened to be at when this digest was published upstream
# (goose --version inside it reports 1.30.0). Building from source at a
# pinned git tag would fix that gap but wasn't attempted here — see the
# migration doc.
#
# When rotating this digest, re-run every livecheck under
# services/agent-runner/internal/*/livecheck against the new one before
# changing it here — the ACP wire format and container behavior were only
# ever verified against this specific digest.
FROM ghcr.io/block/goose@sha256:d85a724ee487425f38ce015323adf2003591268ee515d9018ac89450ed7d3a5a

# The base image has no Node.js at all (confirmed directly: `node`/`npm` are
# both "not found") — needed because the Paca MCP server executor.go spawns
# for every conversation runs via `npx -y @paca-ai/paca-mcp`. Installed from
# NodeSource since Debian bookworm's own apt repos only carry a much older
# Node than @paca-ai/paca-mcp's package.json requires.
#
# Also pre-installs the Paca MCP server globally so `npx -y @paca-ai/paca-mcp`
# resolves from the local install instead of downloading the package on
# every ephemeral sandbox container boot — same rationale as the old
# OpenHands-based Dockerfile's equivalent step.
#
# The base image runs as the unprivileged `goose` user (uid 1000), which
# doesn't own apt's or npm's install dirs — switch to root for the install,
# then drop back so the container still runs unprivileged at runtime.
# Caught by actually running `npx -y @paca-ai/paca-mcp` as the goose user
# after an earlier version of this Dockerfile built successfully: `npm
# install -g` below runs as root, and on this base image root's $HOME is
# already /home/goose (the goose user's home, not /root) — so npm quietly
# wrote its build-time *cache* there too, root-owned. At runtime `npx`
# (running as the unprivileged goose user) refuses to touch a cache
# containing root-owned files and fails with EACCES — and Goose doesn't
# surface that MCP-subprocess failure as an ACP error, it just hangs
# `session/new` forever. The cache is discarded entirely here rather than
# reconciled to the right owner: npx works fine regenerating it fresh as
# goose, and the actually-important part (the global package install
# itself, correctly under root-owned /usr/lib/node_modules) is unaffected.
USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends curl ca-certificates gnupg \
    && curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && npm install -g @paca-ai/paca-mcp \
    && rm -rf /home/goose/.npm \
    && apt-get purge -y curl gnupg \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/*
USER goose

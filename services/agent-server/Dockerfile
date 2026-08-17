# Sandbox image for `llm`-type agent conversations under
# services/agent-runner — the Goose-based replacement for this directory's
# old OpenHands-based Dockerfile (same filename, different content —
# removed along with services/ai-agent itself). This is now the only
# sandbox image in this directory and the one every deployment surface
# (dev/prod/e2e compose, CD) actually builds and publishes.
#
# Pinned by DIGEST to the ghcr.io/aaif-goose/goose:1.46.0 release tag's own
# manifest — this now DOES correspond to a real git tag/changelog entry
# (github.com/aaif-goose/goose's v1.46.0 release, 2026-08-12), unlike the
# previous pin. Two things changed since that previous pin was chosen, both
# confirmed directly rather than assumed:
#   - The GitHub org this was published under was renamed block -> aaif-goose
#     sometime after the previous digest was published (same repo ID,
#     confirmed via the GitHub API — `repos/block/goose` now redirects to
#     `aaif-goose/goose`). ghcr.io does NOT rename container packages when a
#     GitHub org renames, so ghcr.io/block/goose's `main`/`latest` tags are
#     now permanently frozen at whatever they were just before the rename —
#     any future rotation must pull from ghcr.io/aaif-goose/goose instead.
#   - That renamed path publishes proper semver release tags now (confirmed:
#     ghcr.io/aaif-goose/goose:1.46.0 exists and resolves to this digest),
#     not just floating `main`/`main-<shortsha>` CI builds — so this pin
#     could in principle track a tag instead of a bare digest, but stays
#     digest-pinned for the same full-immutability reason as before (a tag
#     can be force-moved upstream; a digest can't).
#
# The previous pin (goose 1.30.0, built 2026-04-06) was replaced because it
# predates Goose's own "skills" feature entirely — confirmed by grepping
# that image's `goose` binary for the string `load_skill`: zero matches,
# vs. 8 in this one. crates/goose/src/skills/mod.rs wasn't added upstream
# until 2026-04-23, about 2.5 weeks after that old digest was built. Paca's
# own bundled `paca` skill (services/api/internal/platform/bundledskills.go)
# now routes specialized skills through Goose's `load_skill` tool, which
# does not exist below this version.
#
# When rotating this digest again, run the full E2E suite
# (services/agent-runner/test/e2e, PACA_E2E=1) against the new one before
# changing it here — the ACP wire format and container behavior were only
# ever verified against this specific digest.
FROM ghcr.io/aaif-goose/goose@sha256:3c961bac6e4dcdb61a35f9ded3fe9a81fba33107dd804c611ebac862f5d72b46

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
# Also installs the `docker` CLI (client only — `docker.io`'s bundled dockerd
# is never started here) so a conversation can run `docker` commands at all.
# sandbox.go points DOCKER_HOST at a dedicated `docker:dind` sidecar it
# starts and tears down alongside this container, on a private
# per-conversation network nothing else can reach — see sandbox.go's
# startDindSidecar doc comment for the isolation properties that gives.
#
# The base image runs as the unprivileged `goose` user (uid 1000) by
# default; this switches to root both for the install below (the base image
# doesn't give goose write access to apt's or npm's install dirs) and,
# since nothing switches back afterward, for the container's runtime user
# too — specifically so a conversation isn't blocked on ordinary
# package-manager operations (apt-get, etc.) the way an earlier version of
# this image was. Every sandbox is a disposable, ephemeral per-conversation
# container torn down at the end of the turn (see sandbox.Manager.Stop),
# not a shared or long-lived host, so the usual reasons to run a service
# unprivileged don't carry the same weight here.
USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends curl ca-certificates gnupg docker.io \
    && curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && npm install -g @paca-ai/paca-mcp \
    && rm -rf /home/goose/.npm \
    && apt-get purge -y curl gnupg \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/*

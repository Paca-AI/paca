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
    && apt-get purge -y gnupg \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/*
# curl is deliberately kept (not purged above like gnupg is) — it's also
# one of the general-purpose tools this next layer exists to provide.

# General-purpose developer tools an agent commonly reaches for mid-task
# that the base image (and the layer above, which only covers what Node/the
# Paca MCP server itself needs) doesn't provide — installed once here so a
# conversation never has to `apt-get install` them itself the way one
# actually did (see the `sudo`/permission-denied failure this whole image's
# root-user switch above was written to fix): ripgrep and tree specifically
# close a real gap, not a hypothetical one — Goose's own default system
# prompt tells the model to "prefer rg" for search and use tree to
# understand a codebase's structure, but neither ever actually shipped in
# this image before now, so every conversation silently fell back to
# slower alternatives without ever being told those two commands were
# missing.
#
# --no-install-recommends throughout, same as the layer above — most
# consequential for python3-pip, whose *recommended* (not required)
# packages pull in python3-dev, a full C header/build-tooling set roughly
# the same size as build-essential itself for a package whose only job here
# is running `pip install`.
#
# Measured directly, not assumed, against this exact package list (`docker
# inspect --format '{{.Size}}'` before and after): ripgrep, tree, jq,
# unzip/zip, less, nano, sqlite3, openssh-client, and python3-pip alone add
# +13MB; the full list below, build-essential/postgresql-client/shellcheck
# included, adds +106MB total — comfortably inside the 256MB budget this
# layer was given, with build-essential (a real C/C++ toolchain — needed
# for native Node/Python extensions that ship source instead of prebuilt
# binaries, a common cause of an otherwise-inexplicable `npm
# install`/`pip install` failure) accounting for most of that. Re-measure
# the same way before adding to this list — several individually-
# small-looking packages can add up faster than they look, python3-pip's
# recommends above being the clearest example already caught here.
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
       ripgrep tree jq unzip zip less nano sqlite3 openssh-client python3-pip \
       build-essential postgresql-client shellcheck \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/*

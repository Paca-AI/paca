# Sandbox image for `llm`-type agent conversations under
# services/agent-runner — the Goose-based replacement for this directory's
# old OpenHands-based Dockerfile (same filename, different content —
# removed along with services/ai-agent itself). This is now the only
# sandbox image in this directory and the one every deployment surface
# (dev/prod/e2e compose, CD) actually builds and publishes.
#
# Pinned by DIGEST to the ghcr.io/aaif-goose/goose:1.50.1 release tag's own
# manifest (bumped from 1.46.0, 2026-09-15) — the latest stable release as of
# this writing, confirmed live via the GitHub API
# (api.github.com/repos/aaif-goose/goose/releases/latest -> tag v1.50.1,
# published 2026-09-14) rather than assumed. 1.50.1 is a same-day patch on
# 1.50.0 ("Revert model updates for patch release" / "Roll back MCP default
# version selection update" — per that release's own changelog); pinning
# 1.50.1 rather than 1.50.0 means this image already reflects that revert,
# not a stale pre-revert state.
#
# The digest itself was resolved from the registry directly, not guessed:
# `ghcr.io/v2/aaif-goose/goose/manifests/1.50.1` (both the `1.50.1` and
# `v1.50.1` tag spellings resolve identically) returned this OCI image
# index's Docker-Content-Digest, and the image was then pulled and run
# end-to-end against that exact digest — `goose --version` reports 1.50.1,
# `goose acp --help` still runs (the ACP server subcommand Paca launches via
# apps/acp-bridge's provider registry), and grepping the binary for
# `load_skill` still finds 8 matches, same count as the 1.46.0 image this
# replaces — Paca's bundled `paca` skill
# (services/api/internal/platform/bundledskills.go) keeps working.
#
# The E2E suite's raw-upstream-image subset (needs only this digest, no
# Node/MCP build) was actually run against it, not just this Dockerfile's
# usual instruction to do so: `PACA_E2E=1 go test ./test/e2e/... -run
# 'TestACPClientAgainstRealGooseContainer|TestSandboxManagerLifecycle'`
# (2026-09-15) — both passed, exercising the real acp.Client handshake
# (types.go's agentCapabilities.loadSession check included) and
# docker.Manager's container lifecycle against this exact digest, not a
# manual curl probe alone. NOT run: the Node/MCP-enabled agentServerImage
# tests (need a full `docker build` of this file) and the dind-sidecar
# tests (privileged, and orthogonal to which Goose image is pinned) — CI's
# test-e2e job covers those; let it run before merging.
#
# Provider surface changed enough between 1.46.0 and 1.50.1 to need a
# separate model-list correction, not just this digest bump — see
# internal/executor/provider.go's providerAPIKeyEnvVar/gooseProviderID
# doc comments and data/llm_models.json (both updated alongside this pin,
# 2026-09-15) for what was cross-checked directly against this version's
# own crates/goose-providers source rather than carried over unverified.
#
# When rotating this digest again, run the full E2E suite
# (services/agent-runner/test/e2e, PACA_E2E=1) against the new one before
# changing it here — the ACP wire format and container behavior were only
# ever verified against this specific digest.
FROM ghcr.io/aaif-goose/goose@sha256:90ed80a60e11ad23b462b455c8541b3dfca48eba0a34f12b4f124f1fd1c91ec3

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
       ripgrep tree jq unzip zip less nano sqlite3 openssh-client openssh-server python3-pip \
       build-essential postgresql-client shellcheck \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/*

# Coding CLIs for provider_cli-type agents — Goose's own "CLI providers"
# feature (GOOSE_PROVIDER=claude-code|codex|cursor-agent|gemini-cli, see
# https://goose-docs.ai/docs/guides/cli-providers/) shells out to whichever
# one of these a given agent is configured for; see
# internal/executor/providercli for how Paca syncs that agent's MCP
# servers/skills into each CLI's own config afterward. Nothing here
# performs any authentication — every CLI still needs its own login (an
# injected API-key env var, or an interactive `<cli> login` run in the
# environment's own terminal — see providercli.Adapter.APIKeyEnvVar /
# CredentialProbePath), which is why these three are only ever installed
# into `provider_cli`-type agents' static environments, never given
# credentials at image-build time.
#
# claude-code/codex/gemini-cli package names confirmed directly against the
# npm registry, and this entire layer (all 4 installs plus `<cli>
# --version` for each) built and ran successfully end to end against a
# node:22-bookworm-slim throwaway image on 2026-09-03 — not a guess:
# claude 2.1.259, codex-cli 0.153.0, gemini 0.58.0, cursor-agent
# 2026.09.02-c22c1a3 all reported their version correctly post-install. Not
# version-pinned, unlike the base Goose image above, since these are meant
# to track upstream fixes/features rather than be protocol-pinned the way
# Goose's own ACP wire format is — expect (and accept) newer versions on
# every image rebuild; re-verify package names specifically (not just that
# `npm install` succeeds) if this layer is ever revisited after a long gap.
#
# cursor-agent has no npm package — Cursor's own installer
# (https://cursor.com/install, confirmed directly by fetching and reading
# it, then actually running it in the verification build above) downloads a
# platform-specific tarball to $HOME/.local/share/cursor-agent and symlinks
# the binary into $HOME/.local/bin/cursor-agent. HOME is pinned to
# /home/goose explicitly here (not assumed inherited) so that install — and
# every CLI's own config-dir resolution at runtime — lands exactly where
# syncProviderCLIConfig's symlink bootstrap expects it
# ($HOME/.claude, $HOME/.codex, $HOME/.cursor, $HOME/.gemini — see
# executor.go's sandboxWorkdir doc comment for why /home/goose is this
# image's real $HOME even while running as root). The installer's own
# symlink is then re-linked onto /usr/local/bin so cursor-agent resolves
# via PATH the same way the three npm-installed CLIs already do.
#
# @agentclientprotocol/claude-agent-acp and @agentclientprotocol/codex-acp
# are two more npm packages, not a replacement for @anthropic-ai/claude-code/
# @openai/codex above — both pairs wrap the SAME underlying claude/codex
# binary. Goose's raw "CLI providers" feature (GOOSE_PROVIDER=claude-code|
# codex) only ever forwards the wrapped CLI's final text: confirmed directly
# in Goose 1.46.0's own source (crates/goose/src/providers/claude_code.rs's
# stream() only parses content_block_delta/text_delta; codex.rs's
# extract_text_from_item only extracts "agent_message" items) — every tool
# call the CLI itself makes is silently dropped before it ever reaches
# Goose, so it can never become an ACP tool_call/tool_call_update
# notification, and (separately) neither raw provider has any real session-
# resume mechanism, so Paca's own conversation memory breaks across
# reconnects. Goose's own provider metadata already marks claude-code
# "[Deprecated: use claude-acp instead]" for exactly this — claude-acp/
# codex-acp are real ACP agent implementations that relay their own
# session/update stream (tool calls included) straight through Goose,
# instead of Goose trying to re-derive it from a wrapped CLI's raw output.
# See executor/provider.go's gooseCLIProviderID for where cli_provider
# "claude-code"/"codex" (Paca's own stable values, unchanged) get translated
# to the "claude-acp"/"codex-acp" GOOSE_PROVIDER values these packages
# register. Verified live (2026-09-04): claude-acp correctly preserved
# conversation memory across a reconnect where claude-code lost it, using
# the exact same real, authenticated `claude` CLI/login state either way —
# see that mapping's own doc comment for the full trace. cursor-agent and
# gemini-cli have no ACP equivalent (confirmed against
# https://goose-docs.ai/docs/guides/acp-providers — only amp-acp/claude-acp/
# codex-acp/pi-acp exist), so they stay on the raw CLI-providers path below,
# unchanged.
ENV HOME=/home/goose
RUN npm install -g @anthropic-ai/claude-code @openai/codex @google/gemini-cli \
       @agentclientprotocol/claude-agent-acp @agentclientprotocol/codex-acp \
    && curl -fsSL https://cursor.com/install | bash \
    && ln -sf /home/goose/.local/bin/cursor-agent /usr/local/bin/cursor-agent

# Real sshd for static environments' layer-4 SSH access (see
# docs/ai-agent/environment-management.md's "Terminal / SSH Access" Phase B
# section and internal/sandbox/environmentssh.go, which starts this
# process via ExecEnvironment once a static environment is running — never
# on an ephemeral per-conversation sandbox, which has no use for it).
# Everything here is static/identical across every environment; the two
# genuinely per-environment pieces (the host key and authorized_keys) are
# generated/written at runtime under the persisted workspace volume
# instead of baked in here — see environmentssh.go's own doc comment for
# why (Kubernetes: a stop/start cycle recreates the Pod and wipes anything
# outside that volume; Docker: ContainerStop preserves the whole
# filesystem regardless, so this only strictly matters on the k8s
# backend, but both backends use the same path for one code path instead
# of two).
#
# PermitRootLogin prohibit-password (not "yes"): only a registered public
# key can log in, never a password — root is the container's only user
# either way (see this Dockerfile's own doc comment above on why this
# image runs as root), so this isn't a new privilege, just a new transport
# to the one the browser terminal/exec already grants. UsePAM no avoids
# pulling in PAM's account/session machinery for a single-user container
# with no real accounts to manage.
RUN mkdir -p /etc/ssh/sshd_config.d
COPY <<'EOF' /etc/ssh/sshd_config.d/paca-environment.conf
Port 2222
PermitRootLogin prohibit-password
PasswordAuthentication no
PubkeyAuthentication yes
UsePAM no
AuthorizedKeysFile /home/paca/workspaces/.ssh_authorized_keys
HostKey /home/paca/workspaces/.ssh_host_keys/ssh_host_ed25519_key
EOF

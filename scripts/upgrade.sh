#!/usr/bin/env bash
# Paca – upgrade script
#
# Updates an existing Paca installation (created by install.sh, or set up
# manually per deploy/README.md) to a new release: refreshes
# docker-compose.yml and the Caddyfile, re-pins image versions in .env when a
# specific version is requested, backfills any .env variables introduced
# since the install was created, then pulls and restarts the stack.
#
# Service scaling from install time (external Postgres, external S3, an
# externally hosted web app, a disabled Agent Runner) is detected from .env
# and re-applied automatically — no need to re-pass the same --scale flags on
# every upgrade. See "Extra arguments" below for scaling that isn't one of
# these.
#
# Upgrading an install that predates Agent Runner (the Go/Goose service that
# replaced the old Python "ai-agent" service)? See "Agent Runner migration"
# below — PACA_AI_AGENT/PACA_AI_AGENT_IMAGE and an AGENT_SERVER_IMAGE pointed
# at the old OpenHands-based sandbox are migrated forward automatically,
# preserving whatever enable/disable choice was already made.
#
# Run this from the directory that holds your docker-compose.yml and .env
# (the directory install.sh created, or wherever you set things up manually).
#
# ── Recommended (interactive) ────────────────────────────────────────────────
#   cd /path/to/your/paca/install
#   curl -fsSL https://github.com/Paca-AI/paca/releases/latest/download/upgrade.sh -o upgrade.sh
#   bash upgrade.sh
#
# ── Non-interactive (CI, scripts, AI coding agents) ──────────────────────────
#   Set PACA_YES=1. This is REQUIRED for unattended use — without it, any
#   prompt this script reaches will block on a `read`, and in a sandbox that
#   provides a pty but no human to type into it, that read never returns.
#   PACA_YES=1 alone is enough to guarantee zero prompts: it proceeds with
#   the upgrade and takes the default (yes) on any conditional .env
#   migration prompt.
#
#   cd /path/to/your/paca/install
#   PACA_YES=1 bash upgrade.sh
#   # or, without a local copy of the script at all:
#   PACA_YES=1 bash <(curl -fsSL https://github.com/Paca-AI/paca/releases/latest/download/upgrade.sh)
#
# ── AI agents: use this script, not a manual `docker compose pull && up` ────
#   This script backs up docker-compose.yml / Caddyfile / .env before
#   overwriting them, only re-pins image tags when you actually asked for a
#   specific version, and backfills any .env variables introduced since the
#   install was created (gateway migration, db-backup service, etc.). A
#   manual pull + restart skips all of that and can leave an installation
#   half-migrated in a way that's hard to diagnose from outside the repo.
#
# ── Environment variable reference ───────────────────────────────────────────
#   PACA_DIR                    Installation directory to upgrade       (default: .)
#   PACA_VERSION                Release tag to upgrade to               (default: the release this
#                                script shipped with, or latest when run from a checkout)
#   PACA_YES                    Skip prompts, use defaults              (set to 1)
#   PACA_PROCEED                Actually run the upgrade (yes/no).      (default: yes)
#                                "no" prints the current vs. target
#                                version and exits without changing
#                                anything — a lightweight version check.
#   PACA_UPDATE_AGENT_IMAGE     Switch AGENT_SERVER_IMAGE off the old   (default: yes)
#                                upstream default when found (yes/no).
#                                Only asked for installs that predate
#                                Paca's own agent-server image.
#   PACA_SSH_BASTION            Enable SSH access to environments       (default: no)
#                                (yes/no). Only asked once, for installs
#                                that predate this option.
#   PACA_SSH_BASTION_PORT_RANGE_START / _END   Only used when enabling  (default: 2200 / 2299)
#                                SSH access as above.
#   PACA_SSH_BASTION_HOST       Only used when enabling SSH access      (default: derived from
#                                as above.                                the existing PUBLIC_URL)
#   PACA_ACKNOWLEDGE_STORAGE_MIGRATION   Proceed even though a real     (default: unset)
#                                bundled MinIO container still exists (see
#                                "Storage backend migration" below) — set to
#                                1 only after migrating its data to RustFS
#                                by hand (docs/deployment/README.md).
#   PACA_KEEP_MINIO              Keep running the existing MinIO         (default: unset)
#                                container instead of migrating (see
#                                "Storage backend migration" below) — a
#                                stopgap, not a long-term choice.
#
# Upgrading an install still running the bundled MinIO container? MinIO
# removed its own images from Docker Hub, so this release replaces it with
# RustFS — see "Storage backend migration" further down in this script and
# docs/deployment/README.md's "Migrating from MinIO to RustFS" section. This
# script refuses to proceed until you say which way you want it: migrate the
# data (PACA_ACKNOWLEDGE_STORAGE_MIGRATION=1, after following the guide
# above) or keep the existing MinIO container running for now
# (PACA_KEEP_MINIO=1) — it will never silently swap your storage backend out
# from under real attachment data, or silently decide for you which of the
# two you meant.
#
# Extra arguments are passed through to the final `docker compose up -d`,
# for any --scale (or other compose flag) beyond what's already inferred
# from .env above, e.g.:
#   bash upgrade.sh --scale <service>=<n>
#   PACA_YES=1 bash upgrade.sh --scale <service>=<n>

set -euo pipefail

# ── Colours ───────────────────────────────────────────────────────────────────

BOLD='\033[1m'; DIM='\033[2m'
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'
RESET='\033[0m'

info()    { echo -e "${GREEN}✔${RESET}  $*"; }
warn()    { echo -e "${YELLOW}!${RESET}  $*"; }
error()   { echo -e "${RED}✖${RESET}  $*" >&2; }
die()     { error "$*"; exit 1; }
heading() { echo -e "\n${BOLD}${CYAN}── $* ${RESET}${DIM}$(printf '─%.0s' {1..40})${RESET}"; }
bold()    { echo -e "${BOLD}$*${RESET}"; }

# ── Helpers ───────────────────────────────────────────────────────────────────

# has_ctty
# /dev/tty is a device node that exists on disk regardless of whether this
# process actually has a controlling terminal to open — `[[ -e /dev/tty ]]`
# is therefore always true on Linux and doesn't tell us anything. Actually
# attempting to open it is the only reliable test, and it must be the
# condition of an if/elif so a failure (no ctty: ENXIO) doesn't trip
# `set -e` and kill the whole script.
has_ctty() { { : </dev/tty; } 2>/dev/null; }

# ask VAR "Question" "default"
# Reads from /dev/tty when stdin is a pipe (curl | bash).
ask() {
    local _var="$1"
    local question="$2"
    local default="${3:-}"
    local prompt

    if [[ -n "$default" ]]; then
        prompt="${BOLD}→${RESET} ${question} ${DIM}[${default}]${RESET}: "
    else
        prompt="${BOLD}→${RESET} ${question}: "
    fi

    local _input=""
    if [[ "${PACA_YES:-0}" == "1" ]]; then
        printf -v "$_var" %s "${default}"
        return
    fi
    if [[ -t 0 ]]; then
        read -r -p "$(echo -e "$prompt")" _input
    elif has_ctty; then
        read -r -p "$(echo -e "$prompt")" _input </dev/tty
    else
        printf -v "$_var" %s "${default}"
        return
    fi
    printf -v "$_var" %s "${_input:-$default}"
}

# yes_no VAR "Question" "y|n"
yes_no() {
    local _var="$1"
    local question="$2"
    local default="${3:-y}"
    local answer=""
    ask answer "$question" "$default"
    local answer_lower
    answer_lower="$(printf '%s' "$answer" | tr '[:upper:]' '[:lower:]')"
    case "$answer_lower" in
        y|yes) printf -v "$_var" %s "yes" ;;
        *)     printf -v "$_var" %s "no"  ;;
    esac
}

# download URL DEST
download() {
    local url="$1" dest="$2"
    if command -v curl &>/dev/null; then
        curl -fsSL --retry 3 "$url" -o "$dest"
    elif command -v wget &>/dev/null; then
        wget -q --tries=3 -O "$dest" "$url"
    else
        die "Neither curl nor wget found. Install one and retry."
    fi
}

# set_env_var FILE VAR VALUE
# Replaces an existing "VAR=..." line in FILE, or appends it if absent.
# Goes through a temp file rather than `sed -i` to avoid GNU/BSD differences.
set_env_var() {
    local file="$1" var="$2" value="$3" tmp
    tmp="$(mktemp)"
    if grep -q "^${var}=" "$file" 2>/dev/null; then
        awk -v var="$var" -v val="$value" -F= '
            $1 == var { print var "=" val; next }
            { print }
        ' "$file" > "$tmp"
    else
        cp "$file" "$tmp"
        printf '%s=%s\n' "$var" "$value" >> "$tmp"
    fi
    mv "$tmp" "$file"
}

# has_env_var FILE VAR
has_env_var() {
    grep -q "^${2}=" "$1" 2>/dev/null
}

# service_has_container SERVICE
# True if this compose project currently has a container for SERVICE, in any
# state (running, stopped, created) — i.e. some previous `up` actually
# created one, as opposed to it having been scaled to 0.
service_has_container() {
    $COMPOSE_CMD --env-file .env ps -a --services 2>/dev/null | grep -qx "$1"
}

# get_env_var FILE VAR
get_env_var() {
    grep "^${2}=" "$1" 2>/dev/null | head -1 | cut -d= -f2-
}

# derive_bare_host URL
# Strips scheme, path, and port from a URL, leaving just the bare
# hostname/IP. Used wherever a plain host is needed as a default to show the
# user (SITE_ADDRESS's own backfill below, the SSH bastion host prompt) —
# PUBLIC_URL is the one place the real reachable address survives even on a
# plain-HTTP install, since SITE_ADDRESS itself is just the literal string
# ":80" in that case (see install.sh's USE_HTTPS=no branch), with no host
# information left in it at all.
derive_bare_host() {
    local host="$1"
    host="${host#http://}"
    host="${host#https://}"
    host="${host%%/*}"
    host="${host%%:*}"
    echo "$host"
}

# ── Version / URL resolution ──────────────────────────────────────────────────

# CD stamps this to the exact tag of the release upgrade.sh ships with (see
# the "Prepare assets" step in .github/workflows/cd.yml), so a plain
# `bash upgrade.sh` upgrades to a version that's guaranteed to exist instead
# of whatever :latest happens to resolve to. The source tree keeps "latest"
# so a checkout run directly still behaves sensibly. Keep this a standalone
# `NAME="value"` assignment — CD's sed matches on that exact shape.
PACA_DEFAULT_VERSION="latest"

PACA_VERSION="${PACA_VERSION:-$PACA_DEFAULT_VERSION}"

if [[ "$PACA_VERSION" == "latest" ]]; then
    RELEASE_BASE="https://github.com/Paca-AI/paca/releases/latest/download"
else
    RELEASE_BASE="https://github.com/Paca-AI/paca/releases/download/${PACA_VERSION}"
fi

# Strip leading 'v' for Docker image tags (v1.2.3 → 1.2.3).
IMAGE_TAG="${PACA_VERSION#v}"

# ── Preflight ─────────────────────────────────────────────────────────────────

echo ""
bold "╔══════════════════════════════════════════════════════════╗"
bold "║         Paca  –  upgrade an existing installation        ║"
bold "╚══════════════════════════════════════════════════════════╝"
echo ""

if ! command -v docker &>/dev/null; then
    die "Docker is not installed. Get it at https://docs.docker.com/get-docker/"
fi
if ! docker info &>/dev/null 2>&1; then
    die "Docker daemon is not running. Start Docker Desktop (or the daemon) and retry."
fi

COMPOSE_CMD=""
if docker compose version &>/dev/null 2>&1; then
    COMPOSE_CMD="docker compose"
elif command -v docker-compose &>/dev/null; then
    COMPOSE_CMD="docker-compose"
else
    die "Docker Compose not found. Install it from https://docs.docker.com/compose/install/"
fi

info "Docker OK  (compose: $COMPOSE_CMD)"

PACA_DIR="${PACA_DIR:-.}"
cd "${PACA_DIR}"
info "Installation directory: $(pwd)"

if [[ ! -f docker-compose.yml || ! -f .env ]]; then
    die "No existing Paca installation found here (missing docker-compose.yml and/or .env). Use install.sh for a fresh install, or set PACA_DIR to point at your install directory."
fi

# ── Current vs target version ─────────────────────────────────────────────────

heading "Version"

CURRENT_TAG="unknown"
if grep -q "^PACA_API_IMAGE=" .env; then
    CURRENT_TAG="$(grep "^PACA_API_IMAGE=" .env | head -1 | sed -e 's/^PACA_API_IMAGE=//' -e 's/.*://')"
fi

info "Current version: ${CURRENT_TAG}"
info "Target version:  ${IMAGE_TAG}"

if [[ -f nginx/gateway.conf ]]; then
    heading "Gateway migration"
    warn "This installation predates the nginx → Caddy gateway migration."
    info "caddy/Caddyfile will be downloaded; nginx/ is no longer referenced by docker-compose.yml."
    info "SITE_ADDRESS and GATEWAY_HTTPS_PORT will be added to .env so Caddy can serve HTTPS automatically."
    info "Once you've confirmed the upgraded stack works, the nginx/ directory can be removed."
fi

PROCEED="yes"
yes_no PROCEED "Proceed with upgrade?" "${PACA_PROCEED:-y}"
if [[ "$PROCEED" != "yes" ]]; then
    warn "Upgrade cancelled."
    exit 0
fi

# ── Storage backend migration ──────────────────────────────────────────────────
# MinIO pulled its own images from Docker Hub and archived its open-source
# repo (see docs/deployment/README.md) — this release's docker-compose.yml
# replaces the bundled "minio" service with "rustfs". An install that still
# has a real minio container from before this release has attachment data
# sitting in its minio_data volume that this script does not migrate
# automatically: downloading the new docker-compose.yml and restarting below
# would, by default, leave the api container pointed at a brand-new, empty
# rustfs container while that data sits inert in the now-orphaned minio_data
# volume — unless told otherwise below.
#
# Three ways forward, in order of preference:
#   1. Migrate the data to RustFS by hand first (docs/deployment/README.md's
#      "Migrating from MinIO to RustFS" section), then re-run with
#      PACA_ACKNOWLEDGE_STORAGE_MIGRATION=1.
#   2. Keep running the existing MinIO container instead — nothing to
#      migrate, nothing removed: STORAGE_PROVIDER/STORAGE_ENDPOINT in .env
#      are left exactly as they are (already pointed at minio:9000), and
#      this run skips --remove-orphans so the container serving them keeps
#      running completely undisturbed alongside the rest of the upgraded
#      stack. A stopgap, not a long-term choice — MinIO's own repo is
#      archived and gets no more security patches, so migrate when you get
#      the chance. Opt in with PACA_KEEP_MINIO=1, or answer the interactive
#      prompt below.
#   3. Do neither: this script refuses to proceed. The default when neither
#      of the above is set, and the only outcome possible under PACA_YES=1
#      unless PACA_ACKNOWLEDGE_STORAGE_MIGRATION or PACA_KEEP_MINIO is also
#      set — an unattended run should never silently guess between "already
#      migrated" and "keep the old one running" on your behalf.
#
# Gated on a real container existing (not just STORAGE_PROVIDER's value) so an
# install that already moved to AWS S3 — and just never removed the old,
# unused minio container — isn't blocked for no reason.
#
# Both checks are read once here and reused everywhere below (through
# "Storage backend migration, continued" further down) instead of
# re-querying .env / `docker compose ps` in every branch — container state
# doesn't change out from under this script between here and there.
STORAGE_PROVIDER_AT_START="$(get_env_var .env STORAGE_PROVIDER)"
HAD_MINIO_CONTAINER="no"
service_has_container minio && HAD_MINIO_CONTAINER="yes"

KEEP_MINIO="no"
if [[ "$STORAGE_PROVIDER_AT_START" != "s3" ]] && [[ "$HAD_MINIO_CONTAINER" == "yes" ]] \
    && [[ "${PACA_ACKNOWLEDGE_STORAGE_MIGRATION:-}" != "1" ]]; then
    if [[ "${PACA_KEEP_MINIO:-}" == "1" ]]; then
        KEEP_MINIO="yes"
    elif [[ "${PACA_YES:-0}" != "1" ]]; then
        warn "This install still has a bundled MinIO container with real attachment data."
        warn "This release switches the bundled object store to RustFS (MinIO removed its own images from Docker Hub — see docs/deployment/README.md)."
        yes_no KEEP_MINIO "Keep running your existing MinIO container for now instead of migrating? (not recommended long-term — MinIO's upstream project is archived and gets no more security patches)" "n"
    fi

    if [[ "$KEEP_MINIO" != "yes" ]]; then
        error "This install still has a bundled MinIO container with real attachment data. Nothing has been changed yet."
        error "Either migrate first — docs/deployment/README.md's \"Migrating from MinIO to RustFS\" section — then re-run with PACA_ACKNOWLEDGE_STORAGE_MIGRATION=1,"
        error "or re-run with PACA_KEEP_MINIO=1 to keep running your existing MinIO container for now instead (not recommended long-term)."
        exit 1
    fi

    warn "Keeping your existing MinIO container: STORAGE_PROVIDER/STORAGE_ENDPOINT in .env stay pointed at it, unchanged, and this run skips --remove-orphans so it keeps running undisturbed. Migrate to RustFS whenever you're ready — docs/deployment/README.md's \"Migrating from MinIO to RustFS\" section."
fi

mkdir -p caddy

# ── Backup and refresh infrastructure files ───────────────────────────────────

heading "Backing up and refreshing infrastructure files"

TS="$(date +%s)"

cp docker-compose.yml "docker-compose.yml.bak.${TS}"
info "Backed up docker-compose.yml → docker-compose.yml.bak.${TS}"
download "${RELEASE_BASE}/docker-compose.yml" docker-compose.yml
info "Downloaded the latest docker-compose.yml."

if [[ -f caddy/Caddyfile ]]; then
    cp caddy/Caddyfile "caddy/Caddyfile.bak.${TS}"
    info "Backed up caddy/Caddyfile → caddy/Caddyfile.bak.${TS}"
fi
download "${RELEASE_BASE}/Caddyfile" caddy/Caddyfile
info "Downloaded the latest caddy/Caddyfile."

ENV_BACKED_UP=0
backup_env_once() {
    if [[ "$ENV_BACKED_UP" == "0" ]]; then
        cp .env ".env.bak.${TS}"
        info "Backed up .env → .env.bak.${TS}"
        ENV_BACKED_UP=1
    fi
}

# repoint_storage_to_rustfs
# Shared by both storage-migration cases below (acknowledged migration, and
# recovering an already-orphaned config) — same action either way, only the
# reason and how it's logged differ, which stays at each call site.
repoint_storage_to_rustfs() {
    backup_env_once
    set_env_var .env STORAGE_PROVIDER "rustfs"
    set_env_var .env STORAGE_ENDPOINT "rustfs:9000"
}

# ── Storage backend migration, continued ──────────────────────────────────────
# Acknowledging the migration (path 1 in the "Storage backend migration"
# section above) means the data has already been copied over by hand and
# this run WILL remove the now-orphaned minio container (--remove-orphans,
# default further below) — so .env must actually point at rustfs afterwards,
# or the api service is left trying to reach a hostname nothing resolves
# anymore. Gated the same way as that section (a real minio container,
# non-S3) rather than on the ack flag alone, so setting
# PACA_ACKNOWLEDGE_STORAGE_MIGRATION=1 on an install that was never blocked
# in the first place (no existing minio container) is a harmless no-op.
if [[ "$STORAGE_PROVIDER_AT_START" != "s3" ]] && [[ "$HAD_MINIO_CONTAINER" == "yes" ]] \
    && [[ "${PACA_ACKNOWLEDGE_STORAGE_MIGRATION:-}" == "1" ]]; then
    repoint_storage_to_rustfs
    info "Migration acknowledged — STORAGE_PROVIDER/STORAGE_ENDPOINT in .env now point at rustfs:9000."
fi

# Recovery: an install can end up with STORAGE_PROVIDER=minio /
# STORAGE_ENDPOINT=minio:9000 in .env while no minio container exists at all
# — confirmed live: the guard above only ever fires when it can see a real
# minio container to protect, so if one was already removed by some earlier,
# unprotected upgrade (a manual `docker compose up --remove-orphans`, a
# script version older than the guard itself, or any other path outside this
# script's control), .env is left silently stale and every future run of
# this script has nothing left to catch. The symptom looks nothing like a
# storage problem at first: the api container crash-loops on "dial tcp:
# lookup minio ... server misbehaving" during its ensure-bucket bootstrap
# step, and because gateway/web/realtime/agent-runner all wait for api to
# become healthy first, the entire stack stays down, not just uploads.
#
# Scoped narrowly on purpose — both the provider AND the exact bundled
# endpoint, not just "provider isn't s3" the way the guard above checks —
# so this never touches an install that deliberately points STORAGE_PROVIDER
# at a self-managed MinIO instance living outside this compose project.
if [[ "$STORAGE_PROVIDER_AT_START" == "minio" ]] \
    && [[ "$(get_env_var .env STORAGE_ENDPOINT)" == "minio:9000" ]] \
    && [[ "$HAD_MINIO_CONTAINER" == "no" ]]; then
    if service_has_container rustfs; then
        repoint_storage_to_rustfs
        warn "Found STORAGE_PROVIDER=minio in .env with no minio container left to serve it — repointed at the existing rustfs container instead so the api service can start."
        warn "If that old minio container's data was never migrated before it disappeared, it may still be sitting unattached in its old volume rather than actually lost — check for it (e.g. \`docker volume ls | grep minio\`) and, if found, mirror it into rustfs by hand: docs/deployment/README.md's \"Migrating from MinIO to RustFS\" section has the mc commands, run against a temporary container pointed at that old volume instead of the (now gone) live one."
    else
        warn "Found STORAGE_PROVIDER=minio in .env with no minio container left to serve it, and no rustfs container either — the api service will keep failing to start until STORAGE_ENDPOINT in .env points at a real, reachable object store. Set STORAGE_PROVIDER/STORAGE_ENDPOINT by hand (or drop any --scale rustfs=0 you're passing, if AWS S3 isn't actually configured), then re-run."
    fi
fi

# ── Agent Runner migration ────────────────────────────────────────────────────
# Installs from before Agent Runner (the Go/Goose service) replaced the old
# Python "ai-agent" service still have PACA_AI_AGENT/PACA_AI_AGENT_IMAGE in
# .env, not the PACA_AGENT_RUNNER/PACA_AGENT_RUNNER_IMAGE docker-compose.yml
# now actually reads — silently ignoring the old names would forget whatever
# enable/disable choice was made at install time and reset it to "enabled".
# Backfilling once here, before anything below reads either name, means every
# later step only ever has to deal with the current names.
if has_env_var .env PACA_AI_AGENT && ! has_env_var .env PACA_AGENT_RUNNER; then
    backup_env_once
    set_env_var .env PACA_AGENT_RUNNER "$(get_env_var .env PACA_AI_AGENT)"
    info "Migrated PACA_AI_AGENT → PACA_AGENT_RUNNER=$(get_env_var .env PACA_AGENT_RUNNER) in .env (preserving your existing choice)."
fi
if has_env_var .env PACA_AI_AGENT_IMAGE && ! has_env_var .env PACA_AGENT_RUNNER_IMAGE; then
    backup_env_once
    _old_tag="$(get_env_var .env PACA_AI_AGENT_IMAGE)"
    _old_tag="${_old_tag##*:}"
    set_env_var .env PACA_AGENT_RUNNER_IMAGE "pacaai/paca-agent-runner:${_old_tag}"
    info "Migrated PACA_AI_AGENT_IMAGE → PACA_AGENT_RUNNER_IMAGE=pacaai/paca-agent-runner:${_old_tag} in .env."
fi

# Only re-pin image tags in .env when a specific version was requested.
# Installs left on the default ":latest" floating tag are already upgraded
# by the pull below — rewriting them here would silently switch a
# deliberately-pinned install onto floating tags, or vice versa.
if [[ "$PACA_VERSION" != "latest" ]]; then
    backup_env_once
    for var in PACA_API_IMAGE PACA_WEB_IMAGE PACA_REALTIME_IMAGE PACA_AGENT_RUNNER_IMAGE; do
        image_name="$(echo "$var" | sed -e 's/^PACA_//' -e 's/_IMAGE$//' | tr '[:upper:]' '[:lower:]' | sed 's/_/-/g')"
        set_env_var .env "$var" "pacaai/paca-${image_name}:${IMAGE_TAG}"
    done
    # Only re-pin AGENT_SERVER_IMAGE if it's already on Paca's own Goose
    # sandbox image — never overwrite a custom value. The old-default
    # migration below handles installs not on that image yet separately.
    if [[ "$(get_env_var .env AGENT_SERVER_IMAGE)" == ghcr.io/paca-ai/paca-agent-server-goose:* ]]; then
        set_env_var .env AGENT_SERVER_IMAGE "ghcr.io/paca-ai/paca-agent-server-goose:${IMAGE_TAG}"
    fi
    info "Pinned image versions in .env to ${IMAGE_TAG}."
else
    info "Using floating :latest images — no image version changes needed."
fi

# Migrate AGENT_SERVER_IMAGE off an old, OpenHands-based default. Agent
# Runner executes conversations through Goose over ACP, not OpenHands's
# agent-server protocol — neither the raw upstream image nor Paca's own
# pre-Agent-Runner build of it works with Agent Runner at all, so an install
# still on either one needs this value changed for agents to work post
# upgrade, not just as a nice-to-have. Matched by repo prefix, not an
# exact-string allowlist of known tags: an earlier version of this check only
# matched "ghcr.io/paca-ai/paca-agent-server:latest" or the *current*
# release's own tag, so an install pinned to any other old version (e.g.
# ":0.12.2", left over from before this repo existed) sailed through
# undetected — AGENT_SERVER_IMAGE stayed on the incompatible OpenHands-based
# image while Agent Runner started fine, and every conversation failed with a
# sandbox that starts, never answers /status, and is gone (AutoRemove) by the
# time diagnostics run. A prefix match catches every tag under either old
# repo, known or not — "ghcr.io/paca-ai/paca-agent-server:" (no trailing
# "-goose") never matches the current good default, which is always
# "...-agent-server-goose:...". Never touches a value the user deliberately
# customized to something outside both old repos. Asked rather than applied
# silently since it's still worth a confirmation, same as the version re-pin
# above.
_current_agent_server_image="$(get_env_var .env AGENT_SERVER_IMAGE)"
_agent_server_image_is_old=0
case "$_current_agent_server_image" in
    ghcr.io/openhands/agent-server:*)   _agent_server_image_is_old=1 ;;
    ghcr.io/paca-ai/paca-agent-server:*) _agent_server_image_is_old=1 ;;
esac
if [[ "$_agent_server_image_is_old" == "1" ]]; then
    heading "Agent-server image"
    info "AGENT_SERVER_IMAGE is still set to an OpenHands-based image (${_current_agent_server_image}), which Agent Runner cannot use."
    info "Paca now ships a Goose-based agent-server image (ghcr.io/paca-ai/paca-agent-server-goose:${IMAGE_TAG}) with the Paca MCP server pre-installed."
    UPDATE_AGENT_IMAGE="yes"
    yes_no UPDATE_AGENT_IMAGE "Switch AGENT_SERVER_IMAGE to ghcr.io/paca-ai/paca-agent-server-goose:${IMAGE_TAG}?" "${PACA_UPDATE_AGENT_IMAGE:-y}"
    if [[ "$UPDATE_AGENT_IMAGE" == "yes" ]]; then
        backup_env_once
        set_env_var .env AGENT_SERVER_IMAGE "ghcr.io/paca-ai/paca-agent-server-goose:${IMAGE_TAG}"
        info "Updated AGENT_SERVER_IMAGE to ghcr.io/paca-ai/paca-agent-server-goose:${IMAGE_TAG}."
    else
        warn "Keeping AGENT_SERVER_IMAGE unchanged — agent conversations will not work against it until you update this yourself."
    fi
fi

# Backfill variables introduced by the nginx → Caddy gateway migration.
# Installations from before that release have neither in .env. SITE_ADDRESS
# defaults to the hostname already in PUBLIC_URL so Caddy requests a
# certificate for the address Paca is actually reachable at, rather than
# silently leaving the upgraded gateway on plain HTTP.
GATEWAY_VARS_ADDED=0
if ! has_env_var .env SITE_ADDRESS; then
    backup_env_once
    _SITE_ADDRESS="$(derive_bare_host "$(get_env_var .env PUBLIC_URL)")"
    _SITE_ADDRESS="${_SITE_ADDRESS:-localhost}"
    set_env_var .env SITE_ADDRESS "$_SITE_ADDRESS"
    info "Added SITE_ADDRESS=${_SITE_ADDRESS} to .env (derived from your existing PUBLIC_URL)."
    GATEWAY_VARS_ADDED=1
fi
if ! has_env_var .env GATEWAY_HTTPS_PORT; then
    backup_env_once
    set_env_var .env GATEWAY_HTTPS_PORT "443"
    info "Added GATEWAY_HTTPS_PORT=443 to .env."
    GATEWAY_VARS_ADDED=1
fi
if [[ "$GATEWAY_VARS_ADDED" == "1" ]]; then
    warn "Ports 80 and 443 must both be reachable from the internet for Let's Encrypt to succeed."
    info "Already behind another TLS terminator (a load balancer, Cloudflare, etc.)? Set SITE_ADDRESS=:80 in .env to keep this gateway on plain HTTP."
fi

# Which optional services should stay scaled to 0 on this upgrade, so you
# don't have to remember and re-pass the same --scale flags you used at
# install time on every single upgrade. Anything not covered below (a custom
# scaling choice of your own) still needs to be passed through via "$@" as
# before.
SCALE_OPTS=()

# Postgres: external DB means the bundled container must never run. This is
# derived purely from DATABASE_URL — the same signal install.sh itself used
# to decide whether to start postgres in the first place — so unlike the
# services below, it needs no separate .env marker or backfill.
if [[ -n "$(get_env_var .env DATABASE_URL)" ]]; then
    SCALE_OPTS+=(--scale postgres=0)
    info "Using an external database (DATABASE_URL is set) — skipping the postgres service."
fi

# Storage: S3, or keeping the existing MinIO container instead (see "Storage
# backend migration" above), both mean the bundled RustFS container must
# never run — same reasoning as postgres above, derived from STORAGE_PROVIDER
# for the S3 case and from KEEP_MINIO for the MinIO one.
if [[ "$(get_env_var .env STORAGE_PROVIDER)" == "s3" ]]; then
    SCALE_OPTS+=(--scale rustfs=0)
    info "Using AWS S3 (STORAGE_PROVIDER=s3 in .env) — skipping the rustfs service."
elif [[ "$KEEP_MINIO" == "yes" ]]; then
    SCALE_OPTS+=(--scale rustfs=0)
    info "Keeping the existing MinIO container — skipping the rustfs service."
fi

# Backfill variables for the db-backup service introduced after this install
# was created. Installations from before that release have none of these in
# .env.
#
# BACKUP_ENABLED is the source of truth when present: it records the explicit
# choice made in install.sh (including "no, don't back up this bundled
# database"), so an upgrade must never override it. Only when it's entirely
# absent — an install that predates BACKUP_ENABLED itself — do we fall back to
# deriving a default from DATABASE_URL, mirroring the bundled-vs-external
# check in install.sh, and then record that derived choice so future upgrades
# read it back instead of re-deriving it.
if has_env_var .env BACKUP_ENABLED; then
    if [[ "$(get_env_var .env BACKUP_ENABLED)" == "false" ]]; then
        SCALE_OPTS+=(--scale db-backup=0)
        info "Database backups are disabled (BACKUP_ENABLED=false in .env) — skipping the db-backup service."
    fi
elif [[ -z "$(get_env_var .env DATABASE_URL)" ]]; then
    backup_env_once
    set_env_var .env BACKUP_ENABLED "true"
    if ! has_env_var .env BACKUP_DIR; then
        set_env_var .env BACKUP_DIR "./backups"
        info "Added BACKUP_DIR=./backups to .env."
    fi
    if ! has_env_var .env BACKUP_RETENTION_DAYS; then
        set_env_var .env BACKUP_RETENTION_DAYS "7"
        info "Added BACKUP_RETENTION_DAYS=7 to .env."
    fi
    if ! has_env_var .env BACKUP_CRON; then
        set_env_var .env BACKUP_CRON "0 2 * * *"
        info "Added BACKUP_CRON=0 2 * * * to .env (runs daily at 02:00 UTC)."
    fi
    _BACKUP_DIR="$(get_env_var .env BACKUP_DIR)"
    _BACKUP_DIR="${_BACKUP_DIR:-./backups}"
    mkdir -p "$_BACKUP_DIR"
    warn "A new db-backup service now writes a daily database dump to ${_BACKUP_DIR}. Disable with --scale db-backup=0 if you already back up this database elsewhere, or set BACKUP_ENABLED=false in .env to keep it off on future upgrades."
else
    backup_env_once
    set_env_var .env BACKUP_ENABLED "false"
    SCALE_OPTS+=(--scale db-backup=0)
    info "Using an external database (DATABASE_URL is set) — skipping the automated db-backup service."
fi

# Used by the web/agent-runner inference below: distinguishes "some services were
# intentionally scaled to 0" from "the whole stack simply isn't running right
# now" (e.g. after `docker compose down`) — in the latter case NO service has
# a container, and without this check everything would look scaled to 0 and
# an upgrade would silently bring nothing back up.
PROJECT_EVER_STARTED=0
[[ -n "$($COMPOSE_CMD --env-file .env ps -a --services 2>/dev/null)" ]] && PROJECT_EVER_STARTED=1

# Web app: PACA_WEB (written to .env by install.sh since this release) is the
# source of truth when present. Installs that predate it get a best-effort
# guess from whether a 'web' container currently exists — only attempted when
# the project has been started before at all, per PROJECT_EVER_STARTED above.
# Either way, the value is written back so future upgrades read it instead of
# re-guessing.
if has_env_var .env PACA_WEB; then
    if [[ "$(get_env_var .env PACA_WEB)" == "external" ]]; then
        SCALE_OPTS+=(--scale web=0)
        info "Web app is externally hosted (PACA_WEB=external in .env) — skipping the web service."
    fi
elif [[ "$PROJECT_EVER_STARTED" == "1" ]]; then
    backup_env_once
    if service_has_container web; then
        set_env_var .env PACA_WEB "bundled"
    else
        set_env_var .env PACA_WEB "external"
        SCALE_OPTS+=(--scale web=0)
        warn "No existing 'web' container found, and this install predates PACA_WEB being tracked in .env — assuming the web app is hosted externally and recording PACA_WEB=external. Wrong? Pass --scale web=1 on this run, then set PACA_WEB=bundled in .env so future upgrades stop guessing."
    fi
fi

# Agent Runner: same reasoning as the web app above, via PACA_AGENT_RUNNER
# (already backfilled from the legacy PACA_AI_AGENT above, if present).
if has_env_var .env PACA_AGENT_RUNNER; then
    if [[ "$(get_env_var .env PACA_AGENT_RUNNER)" == "no" ]]; then
        SCALE_OPTS+=(--scale agent-runner=0)
        info "Agent Runner is disabled (PACA_AGENT_RUNNER=no in .env) — skipping the agent-runner service."
    fi
elif [[ "$PROJECT_EVER_STARTED" == "1" ]]; then
    backup_env_once
    # Checks for a container under either name: an install old enough to
    # have neither PACA_AI_AGENT nor PACA_AGENT_RUNNER in .env may still have
    # a running 'ai-agent' container from before the migration above ran.
    if service_has_container agent-runner || service_has_container ai-agent; then
        set_env_var .env PACA_AGENT_RUNNER "yes"
    else
        set_env_var .env PACA_AGENT_RUNNER "no"
        SCALE_OPTS+=(--scale agent-runner=0)
        warn "No existing 'agent-runner' (or 'ai-agent') container found, and this install predates PACA_AGENT_RUNNER being tracked in .env — assuming Agent Runner is disabled and recording PACA_AGENT_RUNNER=no. Wrong? Pass --scale agent-runner=1 on this run, then set PACA_AGENT_RUNNER=yes in .env so future upgrades stop guessing."
    fi
fi

# Backfill SSH access. Only ever asked once per install: after this block
# runs, SSH_BASTION_PORT_RANGE_START always exists in .env (empty if
# declined), so has_env_var short-circuits every later upgrade — same
# "record the explicit choice" convention as BACKUP_ENABLED/PACA_AGENT_RUNNER
# above, needed here because these two vars have no separate boolean of their
# own (docker-compose.yml's own convention is "empty means off"). Skipped
# entirely when Agent Runner itself is disabled — there's no bastion to
# enable without it — so an agent-runner-less install is never asked and
# never gets these keys written at all.
if [[ "$(get_env_var .env PACA_AGENT_RUNNER)" != "no" ]] && ! has_env_var .env SSH_BASTION_PORT_RANGE_START; then
    backup_env_once
    heading "SSH access"
    echo "  Lets a user ssh straight into a running static environment's own sshd"
    echo "  for pair programming — a dedicated port per environment, published"
    echo "  directly on this host (no relay). Off by default."
    echo ""

    ENABLE_SSH_BASTION="no"
    yes_no ENABLE_SSH_BASTION "Enable SSH access to environments?" "${PACA_SSH_BASTION:-n}"

    if [[ "$ENABLE_SSH_BASTION" == "yes" ]]; then
        SSH_PORT_START="" SSH_PORT_END="" SSH_HOST=""
        ask SSH_PORT_START "SSH bastion port range start" "${PACA_SSH_BASTION_PORT_RANGE_START:-2200}"
        ask SSH_PORT_END "SSH bastion port range end" "${PACA_SSH_BASTION_PORT_RANGE_END:-2299}"
        # agent-runner's own validatePortRange refuses to boot on a bad
        # range — catch it here instead of at a confusing startup failure.
        if ! [[ "$SSH_PORT_START" =~ ^[0-9]+$ && "$SSH_PORT_END" =~ ^[0-9]+$ ]]; then
            die "SSH bastion port range must be numeric (got '${SSH_PORT_START}'-'${SSH_PORT_END}')."
        elif (( 10#$SSH_PORT_END < 10#$SSH_PORT_START )); then
            die "SSH bastion port range end (${SSH_PORT_END}) must be >= start (${SSH_PORT_START})."
        fi
        _SSH_HOST_DEFAULT="$(derive_bare_host "$(get_env_var .env PUBLIC_URL)")"
        ask SSH_HOST "Public host/IP to show in the ssh connect command" "${PACA_SSH_BASTION_HOST:-${_SSH_HOST_DEFAULT:-localhost}}"
        set_env_var .env SSH_BASTION_PORT_RANGE_START "$SSH_PORT_START"
        set_env_var .env SSH_BASTION_PORT_RANGE_END "$SSH_PORT_END"
        set_env_var .env SSH_BASTION_HOST "$SSH_HOST"
        info "SSH access enabled — ports ${SSH_PORT_START}-${SSH_PORT_END} on ${SSH_HOST:-<unset>}."
        warn "Make sure ports ${SSH_PORT_START}-${SSH_PORT_END} are reachable from wherever your users will ssh from (firewall/router forwarding, security group, etc.)."
    else
        set_env_var .env SSH_BASTION_PORT_RANGE_START ""
        set_env_var .env SSH_BASTION_PORT_RANGE_END ""
        info "SSH access left disabled. Enable later by setting SSH_BASTION_PORT_RANGE_START/_END (and optionally SSH_BASTION_HOST) in .env, then re-running docker compose up -d."
    fi
fi

# ── Pull and restart ──────────────────────────────────────────────────────────

heading "Pulling images and restarting"

# AGENT_SERVER_IMAGE isn't a docker-compose service, just an env var
# agent-runner reads and pulls for itself — lazily, the first time a
# conversation actually needs a sandbox (see
# services/agent-runner/internal/sandbox/sandbox.go's ensureImage). Left
# alone, that first conversation after every upgrade pays for a cold pull (or
# times out entirely on a slow link/large image) instead of just running.
# Pulling it here too, best-effort: ensureImage's own pull-on-first-use stays
# the real safety net, so a failure here only costs the win, not correctness.
if [[ "$(get_env_var .env PACA_AGENT_RUNNER)" != "no" ]]; then
    _agent_server_image="$(get_env_var .env AGENT_SERVER_IMAGE)"
    if [[ -n "$_agent_server_image" ]]; then
        info "Pre-pulling agent-server image (${_agent_server_image})..."
        docker pull "$_agent_server_image" || warn "Could not pre-pull ${_agent_server_image} — agent-runner will pull it on first use instead."
    fi
fi

# shellcheck disable=SC2086
$COMPOSE_CMD --env-file .env pull

# --remove-orphans would tear down the MinIO container KEEP_MINIO is
# deliberately keeping running (it's no longer defined in the compose file
# just downloaded, so it reads as an orphan otherwise) — see "Storage
# backend migration" above.
UP_FLAGS=(-d)
if [[ "$KEEP_MINIO" != "yes" ]]; then
    UP_FLAGS+=(--remove-orphans)
fi
# shellcheck disable=SC2086
$COMPOSE_CMD --env-file .env up "${UP_FLAGS[@]}" ${SCALE_OPTS[@]+"${SCALE_OPTS[@]}"} "$@"

# ── Done ──────────────────────────────────────────────────────────────────────

echo ""
bold "╔══════════════════════════════════════════════════════════╗"
bold "║              Paca has been upgraded!                     ║"
bold "╚══════════════════════════════════════════════════════════╝"
echo ""
info "Version: ${IMAGE_TAG}"
echo ""
echo -e "${DIM}Database migrations run automatically on API startup.${RESET}"
echo ""
echo -e "  ${BOLD}Check status:${RESET}  ${COMPOSE_CMD} --env-file .env ps"
echo -e "  ${BOLD}View logs:${RESET}     ${COMPOSE_CMD} --env-file .env logs -f"
echo ""

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
# externally hosted web app, a disabled AI agent) is detected from .env and
# re-applied automatically — no need to re-pass the same --scale flags on
# every upgrade. See "Extra arguments" below for scaling that isn't one of
# these.
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

# Only re-pin image tags in .env when a specific version was requested.
# Installs left on the default ":latest" floating tag are already upgraded
# by the pull below — rewriting them here would silently switch a
# deliberately-pinned install onto floating tags, or vice versa.
if [[ "$PACA_VERSION" != "latest" ]]; then
    backup_env_once
    for var in PACA_API_IMAGE PACA_WEB_IMAGE PACA_REALTIME_IMAGE PACA_AI_AGENT_IMAGE; do
        image_name="$(echo "$var" | sed -e 's/^PACA_//' -e 's/_IMAGE$//' | tr '[:upper:]' '[:lower:]' | sed 's/_/-/g')"
        set_env_var .env "$var" "pacaai/paca-${image_name}:${IMAGE_TAG}"
    done
    # Only re-pin AGENT_SERVER_IMAGE if it's already on Paca's own image —
    # never overwrite a custom value. The old-upstream-image migration below
    # handles the one other known default separately.
    if [[ "$(get_env_var .env AGENT_SERVER_IMAGE)" == ghcr.io/paca-ai/paca-agent-server:* ]]; then
        set_env_var .env AGENT_SERVER_IMAGE "ghcr.io/paca-ai/paca-agent-server:${IMAGE_TAG}"
    fi
    info "Pinned image versions in .env to ${IMAGE_TAG}."
else
    info "Using floating :latest images — no image version changes needed."
fi

# Migrate AGENT_SERVER_IMAGE off the old upstream default. Installs created
# before Paca shipped its own agent-server image (Paca MCP pre-installed,
# openhands-sdk version kept in step with services/ai-agent) have this
# hardcoded to the raw OpenHands image. Only offer to rewrite installs still
# on that exact old default — never touch a value the user deliberately
# customized. Asked rather than applied silently: it changes which image the
# sandbox containers run, which is worth a confirmation like the version
# re-pin above rather than a silent backfill.
OLD_AGENT_SERVER_IMAGE_DEFAULT="ghcr.io/openhands/agent-server:latest-python"
if [[ "$(get_env_var .env AGENT_SERVER_IMAGE)" == "$OLD_AGENT_SERVER_IMAGE_DEFAULT" ]]; then
    heading "Agent-server image"
    info "AGENT_SERVER_IMAGE is still set to the upstream OpenHands image (${OLD_AGENT_SERVER_IMAGE_DEFAULT})."
    info "Paca now ships its own agent-server image (ghcr.io/paca-ai/paca-agent-server:${IMAGE_TAG}) with the Paca MCP server pre-installed, avoiding a cold npm download on every new sandbox."
    UPDATE_AGENT_IMAGE="yes"
    yes_no UPDATE_AGENT_IMAGE "Switch AGENT_SERVER_IMAGE to ghcr.io/paca-ai/paca-agent-server:${IMAGE_TAG}?" "${PACA_UPDATE_AGENT_IMAGE:-y}"
    if [[ "$UPDATE_AGENT_IMAGE" == "yes" ]]; then
        backup_env_once
        set_env_var .env AGENT_SERVER_IMAGE "ghcr.io/paca-ai/paca-agent-server:${IMAGE_TAG}"
        info "Updated AGENT_SERVER_IMAGE to ghcr.io/paca-ai/paca-agent-server:${IMAGE_TAG}."
    else
        info "Keeping AGENT_SERVER_IMAGE unchanged."
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
    _PUBLIC_URL="$(get_env_var .env PUBLIC_URL)"
    _SITE_ADDRESS="${_PUBLIC_URL#http://}"
    _SITE_ADDRESS="${_SITE_ADDRESS#https://}"
    _SITE_ADDRESS="${_SITE_ADDRESS%%/*}"
    _SITE_ADDRESS="${_SITE_ADDRESS%%:*}"
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

# Storage: S3 means the bundled MinIO container must never run — same
# reasoning as postgres above, derived from STORAGE_PROVIDER.
if [[ "$(get_env_var .env STORAGE_PROVIDER)" == "s3" ]]; then
    SCALE_OPTS+=(--scale minio=0)
    info "Using AWS S3 (STORAGE_PROVIDER=s3 in .env) — skipping the minio service."
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

# Used by the web/ai-agent inference below: distinguishes "some services were
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

# AI agent: same reasoning as the web app above, via PACA_AI_AGENT.
if has_env_var .env PACA_AI_AGENT; then
    if [[ "$(get_env_var .env PACA_AI_AGENT)" == "no" ]]; then
        SCALE_OPTS+=(--scale ai-agent=0)
        info "AI agent is disabled (PACA_AI_AGENT=no in .env) — skipping the ai-agent service."
    fi
elif [[ "$PROJECT_EVER_STARTED" == "1" ]]; then
    backup_env_once
    if service_has_container ai-agent; then
        set_env_var .env PACA_AI_AGENT "yes"
    else
        set_env_var .env PACA_AI_AGENT "no"
        SCALE_OPTS+=(--scale ai-agent=0)
        warn "No existing 'ai-agent' container found, and this install predates PACA_AI_AGENT being tracked in .env — assuming the AI agent is disabled and recording PACA_AI_AGENT=no. Wrong? Pass --scale ai-agent=1 on this run, then set PACA_AI_AGENT=yes in .env so future upgrades stop guessing."
    fi
fi

# ── Pull and restart ──────────────────────────────────────────────────────────

heading "Pulling images and restarting"

# shellcheck disable=SC2086
$COMPOSE_CMD --env-file .env pull
# shellcheck disable=SC2086
$COMPOSE_CMD --env-file .env up -d --remove-orphans ${SCALE_OPTS[@]+"${SCALE_OPTS[@]}"} "$@"

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

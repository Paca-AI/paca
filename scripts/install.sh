#!/usr/bin/env bash
# Paca – interactive install script
#
# Downloads the pre-built release artifacts and walks you through setup.
#
# ── Recommended (interactive) ────────────────────────────────────────────────
#   curl -fsSL https://github.com/Paca-AI/paca/releases/latest/download/install.sh -o install.sh
#   bash install.sh
#
# ── Non-interactive (CI, scripts, AI coding agents) ──────────────────────────
#   Set PACA_YES=1. This is REQUIRED for unattended use — without it, any
#   prompt this script reaches will block on a `read`, and in a sandbox that
#   provides a pty but no human to type into it, that read never returns.
#   PACA_YES=1 alone is enough to guarantee zero prompts (every field falls
#   back to a sane default, generating fresh random secrets as needed); add
#   any of the variables below beforehand to steer specific choices instead
#   of accepting the default.
#
#   PACA_YES=1 bash install.sh
#   # or, without a local copy of the script at all:
#   PACA_YES=1 bash <(curl -fsSL https://github.com/Paca-AI/paca/releases/latest/download/install.sh)
#
# ── AI agents: use this script, not a hand-rolled docker-compose/.env ───────
#   This script is the source of truth for a working deployment — it pins
#   compatible image tags, generates every required secret with the right
#   format/length, and writes a `.env` the bundled Caddyfile and compose file
#   expect. Prefer `PACA_YES=1 bash install.sh` (with env overrides below)
#   over reconstructing docker-compose.yml / .env by hand from the README —
#   it is less likely to drift from what a given release actually needs.
#
# ── Environment variable reference ───────────────────────────────────────────
#   Variables named after an actual `.env` key (DATABASE_URL, STORAGE_*,
#   BACKUP_*, PUBLIC_URL, GATEWAY_PORT, ADMIN_*, ENCRYPTION_KEY, JWT_SECRET,
#   POSTGRES_PASSWORD, AGENT_API_KEY, INTERNAL_API_KEY) are written to `.env`
#   verbatim when set. Variables prefixed `PACA_` are installer-only choices
#   that steer which branch of a prompt is taken, with two exceptions —
#   PACA_WEB and PACA_AGENT_RUNNER are also written to `.env` verbatim (see
#   "Web app" / "Agent Runner" below), so upgrade.sh can read the choice back.
#   Every variable below is optional; unset ones get a generated or default
#   value. Secrets left unset are auto-generated and printed at the end of a
#   successful run (and saved in `.env`) — nothing is silently left blank.
#
#   General
#     PACA_DIR                    Installation directory              (default: ./paca)
#     PACA_VERSION                Release tag to install               (default: the release this
#                                  script shipped with, or latest when run from a checkout)
#     PACA_YES                    Skip prompts, use defaults           (set to 1)
#     PACA_START                  Pull images and start after writing  (default: yes)
#                                  config; yes/no.
#
#   Admin account
#     ADMIN_USERNAME               (default: admin; min 3 chars)
#     ADMIN_PASSWORD               (default: auto-generated 16 chars; min 8 if set)
#
#   Encryption (plugin secrets at rest)
#     ENCRYPTION_KEY               64-char lowercase hex (default: auto-generated).
#                                  Reusing an existing DB? You MUST supply its
#                                  original key here — a different one makes
#                                  existing encrypted values unreadable.
#
#   Database
#     DATABASE_URL                 Setting this selects external/managed
#                                  Postgres and suppresses the bundled
#                                  container; leave unset for bundled Postgres.
#     POSTGRES_PASSWORD            Bundled Postgres only (default: auto-generated)
#
#   Database backups (bundled Postgres only; skipped for external DB)
#     BACKUP_ENABLED               true/false                          (default: true)
#     BACKUP_DIR                   (default: ./backups)
#     BACKUP_CRON                  5-field cron, UTC                   (default: "0 2 * * *")
#     BACKUP_RETENTION_DAYS        (default: 7)
#
#   Object storage
#     STORAGE_PROVIDER             minio/s3                            (default: minio)
#     STORAGE_REGION                                                   (default: us-east-1)
#     STORAGE_BUCKET                Required if STORAGE_PROVIDER=s3     (default: paca for minio)
#     STORAGE_ACCESS_KEY_ID         Required if STORAGE_PROVIDER=s3     (default: auto-generated for minio)
#     STORAGE_SECRET_ACCESS_KEY     Required if STORAGE_PROVIDER=s3     (default: auto-generated for minio)
#
#   Network
#     PACA_ADDRESS                 Domain or IP Paca is reachable at    (default: localhost)
#     PACA_HTTPS                   yes/no; ignored for localhost        (default: yes)
#     GATEWAY_PORT                 Only used when serving plain HTTP    (default: 80)
#     PUBLIC_URL                   Full public URL, no trailing slash   (default: derived from the above)
#
#   Web app
#     PACA_WEB                     bundled/external                    (default: bundled)
#
#   Agent Runner
#     PACA_AGENT_RUNNER             yes/no                               (default: yes)
#     AGENT_API_KEY                 (default: auto-generated)
#     INTERNAL_API_KEY              (default: auto-generated)
#
#   SSH access to environments (optional; requires Agent Runner)
#     PACA_SSH_BASTION                    yes/no                                 (default: no)
#     PACA_SSH_BASTION_PORT_RANGE_START   Only asked when PACA_SSH_BASTION=yes    (default: 2200)
#     PACA_SSH_BASTION_PORT_RANGE_END     Only asked when PACA_SSH_BASTION=yes    (default: 2299)
#     PACA_SSH_BASTION_HOST               Only asked when PACA_SSH_BASTION=yes    (default: PACA_ADDRESS)
#
#   PACA_WEB and PACA_AGENT_RUNNER are also written to .env, so upgrade.sh
#   can read the choice back and keep these services scaled to 0 automatically
#   on future upgrades — you won't need to re-pass --scale web=0 / --scale
#   agent-runner=0 by hand each time.
#
#   Other secrets
#     JWT_SECRET                    (default: auto-generated)
#
#   Full example — fully unattended, custom domain, S3, no Agent Runner:
#     PACA_YES=1 \
#     PACA_ADDRESS=paca.example.com \
#     STORAGE_PROVIDER=s3 STORAGE_BUCKET=my-bucket \
#     STORAGE_ACCESS_KEY_ID=AKIA... STORAGE_SECRET_ACCESS_KEY=... \
#     PACA_AGENT_RUNNER=no \
#     ADMIN_PASSWORD='a-strong-password' \
#     bash install.sh

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

# head -c closes the pipe before tr finishes, causing tr to exit with SIGPIPE
# (status 141). pipefail would propagate that non-zero status and kill the
# script. Run each pipeline in a subshell with pipefail disabled so the exit
# code is taken from head (always 0) rather than tr.
rand_hex()    { ( set +o pipefail; LC_ALL=C tr -dc 'a-f0-9'    </dev/urandom | head -c "${1:-32}"; ); }
rand_alnum()  { ( set +o pipefail; LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c "${1:-24}"; ); }

# has_ctty
# /dev/tty is a device node that exists on disk regardless of whether this
# process actually has a controlling terminal to open — `[[ -e /dev/tty ]]`
# is therefore always true on Linux and doesn't tell us anything. Actually
# attempting to open it is the only reliable test, and it must be the
# condition of an if/elif so a failure (no ctty: ENXIO) doesn't trip
# `set -e` and kill the whole script.
has_ctty() { { : </dev/tty; } 2>/dev/null; }

# is_noninteractive
# True exactly when ask() below would resolve to its default without ever
# blocking on real input — PACA_YES=1, or neither stdin nor /dev/tty is
# available to read from. Mirrors ask()'s own branching so fail-fast
# validation (below) only fires for the cases that would otherwise loop
# forever; a genuinely interactive session can just be re-prompted instead.
is_noninteractive() {
    [[ "${PACA_YES:-0}" == "1" ]] && return 0
    [[ -t 0 ]] && return 1
    has_ctty && return 1
    return 0
}

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

# ask_secret VAR "Question"  (no echo, no default shown)
ask_secret() {
    local _var="$1"
    local question="$2"
    local _input=""
    local prompt="${BOLD}→${RESET} ${question} ${DIM}(hidden)${RESET}: "

    if [[ "${PACA_YES:-0}" == "1" ]]; then
        printf -v "$_var" %s ""
        return
    fi
    if [[ -t 0 ]]; then
        read -r -s -p "$(echo -e "$prompt")" _input; echo
    elif has_ctty; then
        read -r -s -p "$(echo -e "$prompt")" _input </dev/tty; echo
    fi
    printf -v "$_var" %s "$_input"
}

# ask_choice VAR "Question" default_idx option1 option2 ...
# default_idx is the 1-based option picked under PACA_YES=1 (or a bare
# Enter) — callers compute it from an env var so a choice can be steered
# non-interactively instead of always landing on option 1.
# Returns the chosen option string.
ask_choice() {
    local _var="$1"
    local question="$2"
    local default_idx="$3"
    shift 3
    local options=("$@")
    local i

    echo -e "\n${question}"
    for i in "${!options[@]}"; do
        echo -e "  ${BOLD}$((i+1))${RESET}) ${options[$i]}"
    done

    local choice=""
    ask choice "Choice" "$default_idx"
    local idx=$(( choice - 1 ))
    if (( idx < 0 || idx >= ${#options[@]} )); then
        warn "Invalid choice, using default (${default_idx})"
        idx=$(( default_idx - 1 ))
    fi
    printf -v "$_var" %s "${options[$idx]}"
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

# get_env_var FILE VAR
get_env_var() {
    grep "^${2}=" "$1" 2>/dev/null | head -1 | cut -d= -f2-
}

# ── Version / URL resolution ──────────────────────────────────────────────────

# CD stamps this to the exact tag of the release install.sh ships with (see
# the "Prepare assets" step in .github/workflows/cd.yml), so a plain
# `bash install.sh` installs a version that's guaranteed to exist instead of
# whatever :latest happens to resolve to. The source tree keeps "latest" so a
# checkout run directly still behaves sensibly. Keep this a standalone
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
bold "║         Paca  –  open-source AI-native project mgmt     ║"
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

# ── Installation directory ────────────────────────────────────────────────────

heading "Installation directory"

PACA_DIR="${PACA_DIR:-./paca}"
ask PACA_DIR "Where should Paca be installed?" "$PACA_DIR"

mkdir -p "${PACA_DIR}/caddy"
cd "${PACA_DIR}"
info "Working directory: $(pwd)"

# ── Admin credentials ─────────────────────────────────────────────────────────

heading "Admin account"

ADMIN_USERNAME="${ADMIN_USERNAME:-admin}"

# validate_username VALUE
# Mirrors the frontend validateUsername: non-empty and at least 3 characters.
validate_username() {
    local v="$1"
    local stripped="${v// /}"
    if [[ -z "$stripped" ]]; then
        error "Username is required."
        return 1
    fi
    if (( ${#stripped} < 3 )); then
        error "Username must be at least 3 characters."
        return 1
    fi
    return 0
}

# Fail fast (rather than looping forever on a `read` nobody will answer) if
# an env-supplied default is already invalid under PACA_YES=1 or any other
# non-interactive run, where every prompt below just re-returns the same
# default. A genuinely interactive run still gets re-prompted below instead.
if is_noninteractive && ! validate_username "$ADMIN_USERNAME"; then
    die "ADMIN_USERNAME (\"${ADMIN_USERNAME}\") is invalid: must be at least 3 characters."
fi

while true; do
    ask ADMIN_USERNAME "Admin username" "$ADMIN_USERNAME"
    if validate_username "$ADMIN_USERNAME"; then
        break
    fi
done

# validate_password VALUE
# Mirrors the frontend validatePassword: non-empty and at least 8 characters.
validate_password() {
    local v="$1"
    if [[ -z "$v" ]]; then
        error "Password is required."
        return 1
    fi
    if (( ${#v} < 8 )); then
        error "Password must be at least 8 characters."
        return 1
    fi
    return 0
}

_GENERATED_PW="$(rand_alnum 16)"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-}"
ADMIN_PASSWORD_GENERATED=0

# If a password was supplied via env var, validate it before proceeding.
if [[ -n "$ADMIN_PASSWORD" ]]; then
    if ! validate_password "$ADMIN_PASSWORD"; then
        die "ADMIN_PASSWORD must be at least 8 characters. Fix the env var or unset it to auto-generate."
    fi
else
    # Interactive: prompt until a valid, non-empty password is entered (or blank → generate).
    ask_secret ADMIN_PASSWORD "Admin password (leave blank to auto-generate)"
    if [[ -z "$ADMIN_PASSWORD" ]]; then
        ADMIN_PASSWORD="$_GENERATED_PW"
        ADMIN_PASSWORD_GENERATED=1
    else
        while ! validate_password "$ADMIN_PASSWORD"; do
            ADMIN_PASSWORD=""
            ask_secret ADMIN_PASSWORD "Admin password (leave blank to auto-generate)"
            if [[ -z "$ADMIN_PASSWORD" ]]; then
                ADMIN_PASSWORD="$_GENERATED_PW"
                ADMIN_PASSWORD_GENERATED=1
                break
            fi
        done
    fi
fi

# ── Encryption key ────────────────────────────────────────────────────────────

heading "Encryption key"

echo "  This key encrypts plugin secrets (OAuth tokens, API keys) stored in the"
echo "  database. If you are connecting to an existing Paca database you MUST"
echo "  supply the original key — a different key makes all existing encrypted"
echo "  values permanently unreadable."
echo ""

ENCRYPTION_KEY="${ENCRYPTION_KEY:-}"
ENCRYPTION_KEY_GENERATED=0

# If a key was supplied via env var, validate it before proceeding — same
# pattern as ADMIN_PASSWORD above, so a bad env value fails loudly instead of
# silently falling through to a prompt that will never be answered.
if [[ -n "$ENCRYPTION_KEY" ]]; then
    if [[ ! "$ENCRYPTION_KEY" =~ ^[a-f0-9]{64}$ ]]; then
        die "Invalid ENCRYPTION_KEY: must be exactly 64 lowercase hex characters (32 bytes). Generate one with: openssl rand -hex 32"
    fi
    info "Using the encryption key from the environment."
else
    ask_secret ENCRYPTION_KEY "Encryption key — 64-char hex (leave blank to generate)"

    if [[ -z "$ENCRYPTION_KEY" ]]; then
        ENCRYPTION_KEY="$(rand_hex 64)"
        ENCRYPTION_KEY_GENERATED=1
        info "Encryption key generated."
    else
        if [[ ! "$ENCRYPTION_KEY" =~ ^[a-f0-9]{64}$ ]]; then
            die "Invalid encryption key: must be exactly 64 lowercase hex characters (32 bytes). Generate one with: openssl rand -hex 32"
        fi
        info "Using the provided encryption key."
    fi
fi

# ── Database ──────────────────────────────────────────────────────────────────

heading "Database"

# Setting DATABASE_URL implies "external" without a separate choice
# variable — an agent that supplies a connection string obviously wants it
# used, and this keeps one env var in sync instead of two.
DB_DEFAULT_IDX=1
[[ -n "${DATABASE_URL:-}" ]] && DB_DEFAULT_IDX=2

DB_CHOICE=""
ask_choice DB_CHOICE "How should Paca store data?" "$DB_DEFAULT_IDX" \
    "Bundled PostgreSQL container (recommended)" \
    "External / managed PostgreSQL (bring your own)"

SCALE_POSTGRES=""
DATABASE_URL_OVERRIDE=""
POSTGRES_PASSWORD_VALUE=""

if [[ "$DB_CHOICE" == *"External"* ]]; then
    SCALE_POSTGRES="--scale postgres=0"
    # ask() prints its default inline (e.g. "[postgres://user:pass@host/db]:"),
    # so passing DATABASE_URL through as the default would echo its embedded
    # credentials to the terminal. Use it directly instead — same pattern as
    # STORAGE_SECRET_ACCESS_KEY below — and only prompt when it's unset.
    if [[ -n "${DATABASE_URL:-}" ]]; then
        DATABASE_URL_OVERRIDE="$DATABASE_URL"
        info "Using the PostgreSQL connection URL from the environment."
    else
        ask DATABASE_URL_OVERRIDE "PostgreSQL connection URL" "postgres://user:pass@host:5432/dbname"
    fi
    if [[ -z "$DATABASE_URL_OVERRIDE" || "$DATABASE_URL_OVERRIDE" == "postgres://user:pass@host:5432/dbname" ]]; then
        die "External PostgreSQL selected but no real DATABASE_URL was given. Set the DATABASE_URL env var or rerun interactively and type a real connection string."
    fi
    # Set a placeholder so the compose's ${POSTGRES_PASSWORD:-changeme} default doesn't matter.
    POSTGRES_PASSWORD_VALUE="not-used-external-db"
    info "External PostgreSQL will be used."
else
    POSTGRES_PASSWORD_VALUE="${POSTGRES_PASSWORD:-$(rand_alnum 20)}"
    info "A bundled PostgreSQL container will be started."
fi

# ── Database backups ──────────────────────────────────────────────────────────

heading "Database backups"

SCALE_DB_BACKUP=""
# Capture the env-supplied enable/disable choice before BACKUP_ENABLED gets
# reused below as the actual (branch-dependent) output value. Normalized to
# lowercase so False/FALSE/0 are recognized too, not just a literal "false".
BACKUP_ENABLED_INPUT="$(printf '%s' "${BACKUP_ENABLED:-true}" | tr '[:upper:]' '[:lower:]')"
BACKUP_ENABLED="true"
BACKUP_DIR="${BACKUP_DIR:-./backups}"
BACKUP_CRON="${BACKUP_CRON:-0 2 * * *}"
BACKUP_RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-7}"

# validate_cron VALUE
# Checks the same field count the db-backup container enforces at startup
# (exactly 5 whitespace-separated fields), plus a basic character check per
# field to catch obviously invalid values (e.g. typos) before they're written
# to .env and silently ignored by crond.
validate_cron() {
    local v="$1"
    local -a fields
    read -ra fields <<< "$v"
    if (( ${#fields[@]} != 5 )); then
        error "Cron schedule must have exactly 5 fields (minute hour day month weekday)."
        return 1
    fi
    local f
    for f in "${fields[@]}"; do
        if [[ ! "$f" =~ ^[0-9*,/-]+$ ]]; then
            error "Invalid cron field '${f}' — only digits and * , - / are supported."
            return 1
        fi
    done
    return 0
}

if [[ -n "$SCALE_POSTGRES" ]]; then
    # db-backup runs pg_dump against the bundled container; an external/managed
    # database is assumed to already have its own backup mechanism.
    SCALE_DB_BACKUP="--scale db-backup=0"
    BACKUP_ENABLED="false"
    info "Using an external database — skipping automated backups (its provider"
    info "likely already handles this). To have Paca back it up instead, set"
    info "BACKUP_DIR/BACKUP_CRON/BACKUP_RETENTION_DAYS in .env and start without"
    info "--scale db-backup=0."
else
    echo "  Writes a gzip-compressed database dump to a directory you choose, on a"
    echo "  cron schedule you set, pruning dumps past the retention period."
    echo ""

    INCLUDE_BACKUPS="yes"
    BACKUP_DEFAULT="y"
    case "$BACKUP_ENABLED_INPUT" in false|0) BACKUP_DEFAULT="n" ;; esac
    yes_no INCLUDE_BACKUPS "Enable automated database backups?" "$BACKUP_DEFAULT"

    if [[ "$INCLUDE_BACKUPS" == "no" ]]; then
        SCALE_DB_BACKUP="--scale db-backup=0"
        BACKUP_ENABLED="false"
        info "Automated backups will be skipped."
    else
        # Fail fast (rather than looping forever on a `read` nobody will
        # answer) if an env-supplied BACKUP_CRON is already invalid. Checked
        # only here — once backups are confirmed to actually run — since an
        # external-DB or backups-disabled install never reads this value, and
        # a bad-but-irrelevant BACKUP_CRON in the environment shouldn't kill
        # an otherwise-valid install.
        if is_noninteractive && ! validate_cron "$BACKUP_CRON"; then
            die "BACKUP_CRON (\"${BACKUP_CRON}\") is invalid: must be 5 whitespace-separated fields of digits and * , - /."
        fi
        ask BACKUP_DIR "Directory to store backups in" "$BACKUP_DIR"
        while true; do
            ask BACKUP_CRON "Backup schedule (cron syntax, interpreted in UTC unless you set TZ in .env later)" "$BACKUP_CRON"
            if validate_cron "$BACKUP_CRON"; then
                break
            fi
        done
        ask BACKUP_RETENTION_DAYS "Days of backups to keep" "$BACKUP_RETENTION_DAYS"
        mkdir -p "$BACKUP_DIR"
        info "Backups will run on '${BACKUP_CRON}' (UTC), written to ${BACKUP_DIR} (kept for ${BACKUP_RETENTION_DAYS} days)."
    fi
fi

# ── Object storage ────────────────────────────────────────────────────────────

heading "Object storage"

# STORAGE_PROVIDER doubles as both the .env output key and the input
# selector here — set it to "s3" or "minio" beforehand to skip this prompt.
STORAGE_DEFAULT_IDX=1
[[ "${STORAGE_PROVIDER:-}" == "s3" ]] && STORAGE_DEFAULT_IDX=2

STORAGE_CHOICE=""
ask_choice STORAGE_CHOICE "Where should file attachments be stored?" "$STORAGE_DEFAULT_IDX" \
    "Self-hosted MinIO (recommended, no cloud account needed)" \
    "AWS S3"

SCALE_MINIO=""
STORAGE_ENDPOINT="minio:9000"
STORAGE_USE_SSL="false"

if [[ "$STORAGE_CHOICE" == *"AWS"* ]]; then
    SCALE_MINIO="--scale minio=0"
    STORAGE_PROVIDER="s3"
    STORAGE_ENDPOINT=""
    STORAGE_USE_SSL="true"

    ask STORAGE_REGION    "AWS region"          "${STORAGE_REGION:-us-east-1}"
    ask STORAGE_BUCKET    "S3 bucket name"      "${STORAGE_BUCKET:-}"
    ask STORAGE_ACCESS_KEY_ID    "AWS access key ID"     "${STORAGE_ACCESS_KEY_ID:-}"
    if [[ -n "${STORAGE_SECRET_ACCESS_KEY:-}" ]]; then
        info "Using the AWS secret access key from the environment."
    else
        ask_secret STORAGE_SECRET_ACCESS_KEY "AWS secret access key"
    fi

    if [[ -z "$STORAGE_BUCKET" ]]; then
        die "S3 bucket name is required."
    fi
    if [[ -z "$STORAGE_ACCESS_KEY_ID" || -z "$STORAGE_SECRET_ACCESS_KEY" ]]; then
        die "AWS credentials are required."
    fi
    info "AWS S3 will be used (bucket: ${STORAGE_BUCKET}, region: ${STORAGE_REGION})."
else
    STORAGE_PROVIDER="minio"
    STORAGE_REGION="${STORAGE_REGION:-us-east-1}"
    STORAGE_BUCKET="${STORAGE_BUCKET:-paca}"
    STORAGE_ACCESS_KEY_ID="${STORAGE_ACCESS_KEY_ID:-$(rand_alnum 16)}"
    STORAGE_SECRET_ACCESS_KEY="${STORAGE_SECRET_ACCESS_KEY:-$(rand_alnum 32)}"
    info "Self-hosted MinIO will be started."
fi

# ── Network ───────────────────────────────────────────────────────────────────

heading "Network"

echo "  Caddy (the gateway) serves HTTPS by default: a trusted Let's Encrypt"
echo "  certificate if you give it a domain name with DNS already pointed at"
echo "  this server, or its own local certificate authority otherwise (an IP"
echo "  address, etc.) — traffic is still encrypted, but browsers will show a"
echo "  trust warning until you have a real domain. \"localhost\" is served"
echo "  over plain HTTP instead, since it never needs (or can get) a certificate."
echo ""

ADDRESS=""
ask ADDRESS "Domain name (recommended) or IP address Paca will be accessible at" "${PACA_ADDRESS:-localhost}"

GATEWAY_PORT="${GATEWAY_PORT:-80}"

if [[ "$ADDRESS" == "localhost" ]]; then
    USE_HTTPS="no"
    info "Using localhost — serving over plain HTTP."
else
    yes_no USE_HTTPS "Serve over HTTPS?" "${PACA_HTTPS:-y}"
fi

if [[ "$USE_HTTPS" == "yes" ]]; then
    SITE_ADDRESS="$ADDRESS"
    PUBLIC_URL="${PUBLIC_URL:-https://${ADDRESS}}"

    info "Caddy will obtain a certificate for ${ADDRESS} on first start — a trusted"
    info "Let's Encrypt certificate if DNS resolves here, otherwise a local one."
    warn "Ports 80 and 443 must both be reachable from the internet for Let's Encrypt to succeed."
else
    SITE_ADDRESS=":80"
    ask GATEWAY_PORT "Gateway port (the port Paca will be accessible on)" "$GATEWAY_PORT"

    # Derive a sensible default public URL from the port.
    if [[ "$GATEWAY_PORT" == "80" ]]; then
        _DEFAULT_PUBLIC_URL="http://${ADDRESS}"
    else
        _DEFAULT_PUBLIC_URL="http://${ADDRESS}:${GATEWAY_PORT}"
    fi

    ask PUBLIC_URL "Public URL (full URL where Paca will be accessible, no trailing slash)" "${PUBLIC_URL:-$_DEFAULT_PUBLIC_URL}"
fi

PUBLIC_URL="${PUBLIC_URL%/}"  # strip trailing slash

# Set COOKIE_SECURE based on whether the URL uses HTTPS.
if [[ "$PUBLIC_URL" == https://* ]]; then
    COOKIE_SECURE="true"
else
    COOKIE_SECURE="false"
fi

# Compute storage public URL only for MinIO (S3 presigned URLs are self-contained).
if [[ "$STORAGE_PROVIDER" == "minio" ]]; then
    STORAGE_PUBLIC_URL="${PUBLIC_URL}/storage"
else
    STORAGE_PUBLIC_URL=""
fi

# ── Web app ───────────────────────────────────────────────────────────────────

heading "Web application"

WEB_DEFAULT_IDX=1
[[ "${PACA_WEB:-}" == "external" ]] && WEB_DEFAULT_IDX=2

WEB_CHOICE=""
ask_choice WEB_CHOICE "How do you want to serve the web app?" "$WEB_DEFAULT_IDX" \
    "Bundled container (recommended – Caddy serves the built React SPA)" \
    "External hosting (S3, CloudFront, Vercel, etc. – only API services run here)"

SCALE_WEB=""
if [[ "$WEB_CHOICE" == *"External"* ]]; then
    SCALE_WEB="--scale web=0"
    PACA_WEB="external"
    echo ""
    warn "The web container will be skipped."
    warn "Build the SPA from source and deploy the dist/ folder to your CDN."
    warn "Point your CDN's API proxy to: ${PUBLIC_URL}/api"
    echo ""
    info "The gateway will still serve /api/, /ws/, and /storage/ routes."
else
    PACA_WEB="bundled"
    info "Bundled web container will be started."
fi

# ── Agent Runner ──────────────────────────────────────────────────────────────

heading "Agent Runner (optional)"

echo "  Agent Runner enables autonomous task execution (llm- and acp-type agents)."
echo "  It requires access to the Docker socket on the host machine."
echo ""

INCLUDE_AGENT_RUNNER="yes"
yes_no INCLUDE_AGENT_RUNNER "Include the Agent Runner service?" "${PACA_AGENT_RUNNER:-y}"

SCALE_AGENT_RUNNER=""
AGENT_API_KEY="${AGENT_API_KEY:-$(rand_hex 32)}"
INTERNAL_API_KEY="${INTERNAL_API_KEY:-$(rand_hex 32)}"

if [[ "$INCLUDE_AGENT_RUNNER" == "no" ]]; then
    SCALE_AGENT_RUNNER="--scale agent-runner=0"
    info "Agent Runner will be skipped."
else
    info "Agent Runner will be included."
fi

SSH_BASTION_PORT_RANGE_START=""
SSH_BASTION_PORT_RANGE_END=""
SSH_BASTION_HOST=""
if [[ "$INCLUDE_AGENT_RUNNER" == "yes" ]]; then
    echo ""
    echo "  SSH access lets a user ssh straight into a running static environment's"
    echo "  own sshd for pair programming — a dedicated port per environment,"
    echo "  published directly on this host (no relay). Off by default."
    echo ""

    ENABLE_SSH_BASTION="no"
    yes_no ENABLE_SSH_BASTION "Enable SSH access to environments?" "${PACA_SSH_BASTION:-n}"

    if [[ "$ENABLE_SSH_BASTION" == "yes" ]]; then
        ask SSH_BASTION_PORT_RANGE_START "SSH bastion port range start" "${PACA_SSH_BASTION_PORT_RANGE_START:-2200}"
        ask SSH_BASTION_PORT_RANGE_END "SSH bastion port range end" "${PACA_SSH_BASTION_PORT_RANGE_END:-2299}"
        # agent-runner's own validatePortRange refuses to boot on a bad
        # range — catch it here instead of at a confusing startup failure.
        if ! [[ "$SSH_BASTION_PORT_RANGE_START" =~ ^[0-9]+$ && "$SSH_BASTION_PORT_RANGE_END" =~ ^[0-9]+$ ]]; then
            die "SSH bastion port range must be numeric (got '${SSH_BASTION_PORT_RANGE_START}'-'${SSH_BASTION_PORT_RANGE_END}')."
        elif (( 10#$SSH_BASTION_PORT_RANGE_END < 10#$SSH_BASTION_PORT_RANGE_START )); then
            die "SSH bastion port range end (${SSH_BASTION_PORT_RANGE_END}) must be >= start (${SSH_BASTION_PORT_RANGE_START})."
        fi
        ask SSH_BASTION_HOST "Public host/IP to show in the ssh connect command" "${PACA_SSH_BASTION_HOST:-$ADDRESS}"
        warn "Make sure ports ${SSH_BASTION_PORT_RANGE_START}-${SSH_BASTION_PORT_RANGE_END} are reachable from wherever your users will ssh from (firewall/router forwarding, security group, etc.)."
    else
        info "SSH access will be left disabled. Enable later by setting SSH_BASTION_PORT_RANGE_START/_END in .env and restarting."
    fi
fi

# ── Download release assets ───────────────────────────────────────────────────

heading "Downloading release assets"

if [[ -f docker-compose.yml ]]; then
    warn "docker-compose.yml already exists — skipping download."
else
    info "Downloading docker-compose.yml..."
    download "${RELEASE_BASE}/docker-compose.yml" docker-compose.yml
fi

if [[ -f caddy/Caddyfile ]]; then
    warn "caddy/Caddyfile already exists — skipping download."
else
    info "Downloading caddy/Caddyfile..."
    download "${RELEASE_BASE}/Caddyfile" caddy/Caddyfile
fi

# ── Generate .env ─────────────────────────────────────────────────────────────

heading "Generating .env"

if [[ -f .env ]]; then
    warn ".env already exists."
    KEEP_ENV="yes"
    yes_no KEEP_ENV "Keep existing .env?" "y"
    if [[ "$KEEP_ENV" == "yes" ]]; then
        warn "Keeping existing .env. Delete it and re-run to regenerate."
    else
        mv .env ".env.bak.$(date +%s)"
        warn "Old .env backed up."
        KEEP_ENV="no"
    fi
else
    KEEP_ENV="no"
fi

if [[ "$KEEP_ENV" == "no" ]]; then
    JWT_SECRET="${JWT_SECRET:-$(rand_hex 32)}"

    cat >.env <<EOF
# ── Paca environment ──────────────────────────────────────────────────────────
# Generated by install.sh  $(date -u '+%Y-%m-%dT%H:%M:%SZ')
#
# To reconfigure: edit this file, then run:
#   ${COMPOSE_CMD} --env-file .env up -d
# ─────────────────────────────────────────────────────────────────────────────

# ── Image versions ────────────────────────────────────────────────────────────
PACA_API_IMAGE=pacaai/paca-api:${IMAGE_TAG}
PACA_WEB_IMAGE=pacaai/paca-web:${IMAGE_TAG}
PACA_REALTIME_IMAGE=pacaai/paca-realtime:${IMAGE_TAG}
PACA_AGENT_RUNNER_IMAGE=pacaai/paca-agent-runner:${IMAGE_TAG}

ENVIRONMENT=production
GATEWAY_PORT=${GATEWAY_PORT}
GATEWAY_HTTPS_PORT=443
# Caddy site address. A domain or IP gets HTTPS automatically (a trusted
# Let's Encrypt certificate for a real domain, Caddy's own local certificate
# authority otherwise). Set to ":80" to disable HTTPS and serve plain HTTP.
SITE_ADDRESS=${SITE_ADDRESS}

# ── Public URL ────────────────────────────────────────────────────────────────
PUBLIC_URL=${PUBLIC_URL}

# ── Admin credentials ────────────────────────────────────────────────────────
ADMIN_USERNAME=${ADMIN_USERNAME}
ADMIN_PASSWORD=${ADMIN_PASSWORD}

# ── JWT ───────────────────────────────────────────────────────────────────────
JWT_SECRET=${JWT_SECRET}
JWT_ACCESS_TTL=15m
JWT_REFRESH_TTL=168h
JWT_REFRESH_SESSION_TTL=24h
COOKIE_SECURE=${COOKIE_SECURE}

# ── Database ─────────────────────────────────────────────────────────────────
POSTGRES_DB=paca
POSTGRES_USER=paca
POSTGRES_PASSWORD=${POSTGRES_PASSWORD_VALUE}
# Override: leave blank to use the bundled postgres container above.
DATABASE_URL=${DATABASE_URL_OVERRIDE}

# ── Database backups ─────────────────────────────────────────────────────────
# BACKUP_ENABLED records your choice below so upgrade.sh can honor it later
# instead of guessing — flip to false (and optionally --scale db-backup=0) to
# turn backups off permanently.
BACKUP_ENABLED=${BACKUP_ENABLED}
BACKUP_DIR=${BACKUP_DIR}
# Standard 5-field cron syntax, interpreted in UTC. Uncomment TZ below to change.
BACKUP_CRON=${BACKUP_CRON}
BACKUP_RETENTION_DAYS=${BACKUP_RETENTION_DAYS}
# TZ=America/New_York

# ── Cache (Valkey) ────────────────────────────────────────────────────────────
# Leave blank to use the bundled Valkey container.
REDIS_URL=

# ── Object storage ────────────────────────────────────────────────────────────
STORAGE_PROVIDER=${STORAGE_PROVIDER}
STORAGE_ENDPOINT=${STORAGE_ENDPOINT}
STORAGE_PUBLIC_URL=${STORAGE_PUBLIC_URL}
STORAGE_REGION=${STORAGE_REGION}
STORAGE_BUCKET=${STORAGE_BUCKET}
STORAGE_ACCESS_KEY_ID=${STORAGE_ACCESS_KEY_ID}
STORAGE_SECRET_ACCESS_KEY=${STORAGE_SECRET_ACCESS_KEY}
STORAGE_USE_SSL=${STORAGE_USE_SSL}

# ── Encryption ────────────────────────────────────────────────────────────────
# 64-char hex string used to encrypt plugin secrets at rest.
ENCRYPTION_KEY=${ENCRYPTION_KEY}

# ── Agent Runner ─────────────────────────────────────────────────────────────
# Both keys below must match across api and agent-runner services.
AGENT_API_KEY=${AGENT_API_KEY}
INTERNAL_API_KEY=${INTERNAL_API_KEY}
AGENT_SERVER_IMAGE=ghcr.io/paca-ai/paca-agent-server-goose:${IMAGE_TAG}
PORT_POOL_START=10000
PORT_POOL_SIZE=100
WORKER_CONCURRENCY=10

# ── SSH access (optional) ────────────────────────────────────────────────────
# Off by default (both empty — see docker-compose.yml's own comment on these).
# Lets a user ssh straight into a running static environment's own sshd — see
# docs/ai-agent/environment-management.md's "Terminal / SSH Access" section.
SSH_BASTION_PORT_RANGE_START=${SSH_BASTION_PORT_RANGE_START}
SSH_BASTION_PORT_RANGE_END=${SSH_BASTION_PORT_RANGE_END}
SSH_BASTION_HOST=${SSH_BASTION_HOST}

# ── Service topology ─────────────────────────────────────────────────────────
# Recorded so upgrade.sh can keep these services scaled to 0 automatically on
# future upgrades, instead of you having to re-pass --scale flags every time.
PACA_WEB=${PACA_WEB}
PACA_AGENT_RUNNER=${INCLUDE_AGENT_RUNNER}

# ── Logging ───────────────────────────────────────────────────────────────────
LOG_LEVEL=info
EOF
    info ".env written."
fi

# ── Confirm and start ─────────────────────────────────────────────────────────

heading "Summary"

echo ""
echo -e "  ${BOLD}Directory   ${RESET}$(pwd)"
echo -e "  ${BOLD}Version     ${RESET}${PACA_VERSION}"
echo -e "  ${BOLD}Public URL  ${RESET}${PUBLIC_URL}"
echo -e "  ${BOLD}HTTPS       ${RESET}$( [[ "$USE_HTTPS" == "yes" ]] && echo "Enabled (${SITE_ADDRESS})" || echo "Disabled (plain HTTP)" )"
echo -e "  ${BOLD}Database    ${RESET}$( [[ -n "$SCALE_POSTGRES" ]] && echo "External PostgreSQL" || echo "Bundled PostgreSQL container" )"
echo -e "  ${BOLD}Backups     ${RESET}$( [[ -n "$SCALE_DB_BACKUP" ]] && echo "Disabled" || echo "'${BACKUP_CRON}' UTC → ${BACKUP_DIR} (kept ${BACKUP_RETENTION_DAYS}d)" )"
echo -e "  ${BOLD}Storage     ${RESET}$( [[ "$STORAGE_PROVIDER" == "s3" ]] && echo "AWS S3 (${STORAGE_BUCKET})" || echo "Self-hosted MinIO" )"
echo -e "  ${BOLD}Web app     ${RESET}$( [[ -n "$SCALE_WEB" ]] && echo "External / CDN (container skipped)" || echo "Bundled container" )"
echo -e "  ${BOLD}Agent Runner${RESET}$( [[ -n "$SCALE_AGENT_RUNNER" ]] && echo "Disabled" || echo "Enabled" )"
echo -e "  ${BOLD}Admin user  ${RESET}${ADMIN_USERNAME}"
echo ""

START="yes"
yes_no START "Pull images and start Paca now?" "${PACA_START:-y}"

if [[ "$START" != "yes" ]]; then
    echo ""
    warn "Installation files are ready. Start Paca manually with:"
    echo ""
    bold "  cd $(pwd)"
    bold "  ${COMPOSE_CMD} --env-file .env up -d ${SCALE_POSTGRES} ${SCALE_DB_BACKUP} ${SCALE_MINIO} ${SCALE_WEB} ${SCALE_AGENT_RUNNER} --pull always"
    echo ""
    exit 0
fi

# ── Start the stack ───────────────────────────────────────────────────────────

heading "Starting Paca"

# AGENT_SERVER_IMAGE isn't a docker-compose service, just an env var
# agent-runner reads and pulls for itself — lazily, the first time a
# conversation actually needs a sandbox (see
# services/agent-runner/internal/sandbox/sandbox.go's ensureImage), so
# `--pull always` below never touches it. Left alone, the very first
# conversation on a fresh install pays for that cold pull (or times out
# entirely on a slow link/large image) instead of just running. Pulling it
# here too, best-effort: ensureImage's own pull-on-first-use stays the real
# safety net, so a failure here only costs the win, not correctness.
#
# Read back from .env rather than assuming the fresh-install default: when an
# existing .env was kept (KEEP_ENV=yes above), AGENT_SERVER_IMAGE may already
# be pinned to something else, and pre-pulling the wrong ref would fetch an
# image this deployment won't use while leaving the actually-configured one
# cold — the exact outcome this feature exists to prevent. Falls back to the
# same default the freshly-written .env above uses when the kept .env
# predates AGENT_SERVER_IMAGE entirely. Mirrors upgrade.sh's own
# get_env_var-based read of the effective value.
if [[ "$INCLUDE_AGENT_RUNNER" != "no" ]]; then
    _agent_server_image="$(get_env_var .env AGENT_SERVER_IMAGE)"
    _agent_server_image="${_agent_server_image:-ghcr.io/paca-ai/paca-agent-server-goose:${IMAGE_TAG}}"
    info "Pre-pulling agent-server image (${_agent_server_image})..."
    docker pull "$_agent_server_image" || warn "Could not pre-pull ${_agent_server_image} — agent-runner will pull it on first use instead."
fi

SCALE_OPTS=()
[[ -n "$SCALE_POSTGRES"  ]] && SCALE_OPTS+=($SCALE_POSTGRES)
[[ -n "$SCALE_DB_BACKUP" ]] && SCALE_OPTS+=($SCALE_DB_BACKUP)
[[ -n "$SCALE_MINIO"     ]] && SCALE_OPTS+=($SCALE_MINIO)
[[ -n "$SCALE_WEB"       ]] && SCALE_OPTS+=($SCALE_WEB)
[[ -n "$SCALE_AGENT_RUNNER" ]] && SCALE_OPTS+=($SCALE_AGENT_RUNNER)

# shellcheck disable=SC2086
$COMPOSE_CMD --env-file .env up -d ${SCALE_OPTS[@]+"${SCALE_OPTS[@]}"} --pull always

# ── Done ──────────────────────────────────────────────────────────────────────

echo ""
bold "╔══════════════════════════════════════════════════════════╗"
bold "║               Paca is starting up!                      ║"
bold "╚══════════════════════════════════════════════════════════╝"
echo ""
info "Web UI:     ${PUBLIC_URL}"
info "Admin user: ${ADMIN_USERNAME}"

if [[ "$ADMIN_PASSWORD_GENERATED" == "1" ]]; then
    echo ""
    warn "Your admin password was auto-generated. Save it now:"
    bold "    ADMIN_PASSWORD=${ADMIN_PASSWORD}"
    echo ""
    warn "It is also stored in $(pwd)/.env"
fi

if [[ "$ENCRYPTION_KEY_GENERATED" == "1" ]]; then
    echo ""
    warn "Your encryption key was auto-generated. Back it up — you will need"
    warn "this exact key to access encrypted data if you ever migrate or restore"
    warn "the database:"
    bold "    ENCRYPTION_KEY=${ENCRYPTION_KEY}"
    echo ""
    warn "It is also stored in $(pwd)/.env"
fi

echo ""
echo -e "${DIM}Services may take up to a minute to become healthy.${RESET}"
echo ""
echo -e "  ${BOLD}Check status:${RESET}  ${COMPOSE_CMD} --env-file .env ps"
echo -e "  ${BOLD}View logs:${RESET}     ${COMPOSE_CMD} --env-file .env logs -f"
echo -e "  ${BOLD}Stop:${RESET}          ${COMPOSE_CMD} --env-file .env down"
echo ""

#!/usr/bin/env bash
# Paca – object-storage migration script
#
# Copies every object in Paca's bucket (attachments, avatars, plugin bundles
# when PLUGINS_STORE=s3, ...) from the storage backend your install currently
# uses to a different one, verifies the copy, and then points .env at the new
# backend:
#
#   --to s3      bundled RustFS  →  AWS S3 (or any other S3-compatible service)
#   --to rustfs  AWS S3 / S3-compatible  →  bundled RustFS container
#
# The source bucket is NEVER modified or deleted — if anything looks wrong you
# can point .env back at it (a .env.bak.<timestamp> is written before .env is
# touched, and is restored automatically if the script fails part-way).
#
# Run this from the directory that holds your docker-compose.yml and .env (the
# directory install.sh created). Requires Docker; copying is done by MinIO
# Client (`mc`, quay.io/minio/mc) running in a throwaway container attached to
# the compose network, because the bundled RustFS port is not published to the
# host.
#
# ── Usage ────────────────────────────────────────────────────────────────────
#   bash migrate-storage.sh --to s3
#   bash migrate-storage.sh --to rustfs
#   bash migrate-storage.sh --to s3 --dry-run     # list what would be copied
#
# ── Environment variable reference ───────────────────────────────────────────
#   PACA_DIR                     Installation directory               (default: .)
#   PACA_MIGRATE_TO              s3 | rustfs (same as --to)
#   PACA_YES                     Skip prompts, use defaults           (set to 1)
#   PACA_STOP_API                Stop the api service during the copy (default: yes)
#                                so nothing is written mid-migration.
#                                Set to "no" only if you accept that files
#                                uploaded during the copy may be missed.
#   PACA_SWITCH_ENV              Update .env + restart api afterwards (default: yes)
#   PACA_MC_IMAGE                MinIO Client image        (default: quay.io/minio/mc)
#   PACA_COMPOSE_NETWORK         Docker network of the stack. Auto-detected from
#                                the running rustfs/api container if unset.
#
#   Destination (the backend you are migrating TO):
#   DEST_STORAGE_BUCKET          --to s3: required.  --to rustfs: default "paca"
#   DEST_STORAGE_ACCESS_KEY_ID      --to s3: required.  --to rustfs: generated if unset
#   DEST_STORAGE_SECRET_ACCESS_KEY  --to s3: required.  --to rustfs: generated if unset
#   DEST_STORAGE_REGION          default: us-east-1
#   DEST_STORAGE_ENDPOINT        --to s3 only. Empty = AWS regional endpoint. Set
#                                host[:port] for another S3-compatible service
#                                (Cloudflare R2, Backblaze B2, a self-managed MinIO ...)
#   DEST_STORAGE_USE_SSL         --to s3 only. true/false          (default: true)
#
# The source is always read from the current STORAGE_* values in .env.
#
# Examples:
#   DEST_STORAGE_BUCKET=my-paca-bucket \
#   DEST_STORAGE_ACCESS_KEY_ID=AKIA... DEST_STORAGE_SECRET_ACCESS_KEY=... \
#   DEST_STORAGE_REGION=eu-west-1 bash migrate-storage.sh --to s3
#
#   bash migrate-storage.sh --to rustfs

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

has_ctty() { { : </dev/tty; } 2>/dev/null; }

# yes_no VAR "Question" "y|n" — honours PACA_YES=1, reads /dev/tty when piped.
yes_no() {
    local _var="$1" question="$2" default="${3:-y}" answer=""
    if [[ "${PACA_YES:-0}" == "1" ]]; then
        answer="$default"
    elif [[ -t 0 ]]; then
        read -r -p "$(echo -e "${BOLD}?${RESET}  ${question} [${default}] ")" answer || true
    elif has_ctty; then
        read -r -p "$(echo -e "${BOLD}?${RESET}  ${question} [${default}] ")" answer </dev/tty || true
    fi
    answer="$(printf '%s' "${answer:-$default}" | tr '[:upper:]' '[:lower:]')"
    case "$answer" in y|yes) printf -v "$_var" %s yes ;; *) printf -v "$_var" %s no ;; esac
}

# get_env_var FILE VAR — value or nothing; never fails under `set -e`.
get_env_var() {
    { grep "^${2}=" "$1" 2>/dev/null || true; } | head -1 | cut -d= -f2-
}

# set_env_var FILE VAR VALUE — replace or append. Values are passed through
# ENVIRON so backslashes / special characters in secrets survive awk.
set_env_var() {
    local file="$1" var="$2" value="$3" tmp
    tmp="$(mktemp)"
    if grep -q "^${var}=" "$file" 2>/dev/null; then
        VAL="$value" awk -v var="$var" -F= '
            $1 == var { print var "=" ENVIRON["VAL"]; next }
            { print }
        ' "$file" > "$tmp"
    else
        cp "$file" "$tmp"
        printf '%s=%s\n' "$var" "$value" >> "$tmp"
    fi
    cat "$tmp" > "$file"   # keep the file's mode/ownership
    rm -f "$tmp"
}

rand_hex() { ( set +o pipefail; LC_ALL=C tr -dc 'a-f0-9' </dev/urandom | head -c "${1:-32}"; ); }

# ── Arguments ─────────────────────────────────────────────────────────────────

TARGET="${PACA_MIGRATE_TO:-}"
DRY_RUN="no"
while [[ $# -gt 0 ]]; do
    case "$1" in
        --to)        TARGET="${2:-}"; shift 2 ;;
        --to=*)      TARGET="${1#--to=}"; shift ;;
        --dry-run)   DRY_RUN="yes"; shift ;;
        -h|--help)   sed -n '2,/^set -euo/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//'; exit 0 ;;
        *)           die "Unknown argument: $1 (see --help)" ;;
    esac
done

case "$TARGET" in
    s3|rustfs) ;;
    "") die "Missing --to. Use '--to s3' or '--to rustfs' (see --help)." ;;
    *)  die "Invalid --to '$TARGET': must be 's3' or 'rustfs'." ;;
esac

# ── Preflight ─────────────────────────────────────────────────────────────────

echo ""
bold "Paca – migrate object storage (→ ${TARGET})"
echo ""

command -v docker &>/dev/null || die "Docker is not installed."
docker info &>/dev/null 2>&1 || die "Docker daemon is not running."

if docker compose version &>/dev/null 2>&1; then
    COMPOSE_CMD="docker compose"
elif command -v docker-compose &>/dev/null; then
    COMPOSE_CMD="docker-compose"
else
    die "Docker Compose not found."
fi

PACA_DIR="${PACA_DIR:-.}"
cd "$PACA_DIR"
[[ -f docker-compose.yml && -f .env ]] \
    || die "No Paca installation here (missing docker-compose.yml and/or .env). Run from your install directory or set PACA_DIR."
info "Installation directory: $(pwd)"

MC_IMAGE="${PACA_MC_IMAGE:-quay.io/minio/mc}"

# ── Source (current .env) ─────────────────────────────────────────────────────

SRC_PROVIDER="$(get_env_var .env STORAGE_PROVIDER)"; SRC_PROVIDER="${SRC_PROVIDER:-rustfs}"
SRC_ENDPOINT="$(get_env_var .env STORAGE_ENDPOINT)"
SRC_BUCKET="$(get_env_var .env STORAGE_BUCKET)";     SRC_BUCKET="${SRC_BUCKET:-paca}"
SRC_REGION="$(get_env_var .env STORAGE_REGION)";     SRC_REGION="${SRC_REGION:-us-east-1}"
SRC_AK="$(get_env_var .env STORAGE_ACCESS_KEY_ID)"
SRC_SK="$(get_env_var .env STORAGE_SECRET_ACCESS_KEY)"
SRC_SSL="$(get_env_var .env STORAGE_USE_SSL)"

if [[ "$SRC_PROVIDER" == "$TARGET" ]]; then
    die "STORAGE_PROVIDER in .env is already '$SRC_PROVIDER' — nothing to migrate to '$TARGET'."
fi
if [[ "$SRC_PROVIDER" != "rustfs" && "$SRC_PROVIDER" != "s3" ]]; then
    die "Unsupported current STORAGE_PROVIDER '$SRC_PROVIDER' (this script handles rustfs and s3)."
fi
[[ -n "$SRC_AK" && -n "$SRC_SK" ]] || die "STORAGE_ACCESS_KEY_ID / STORAGE_SECRET_ACCESS_KEY are not set in .env."

if [[ "$SRC_PROVIDER" == "rustfs" ]]; then
    SRC_ENDPOINT="${SRC_ENDPOINT:-rustfs:9000}"
    SRC_SSL="${SRC_SSL:-false}"
else
    SRC_SSL="${SRC_SSL:-true}"
    if [[ -z "$SRC_ENDPOINT" ]]; then
        SRC_ENDPOINT="s3.amazonaws.com"
    fi
fi
SRC_SCHEME="http"; [[ "$SRC_SSL" == "true" ]] && SRC_SCHEME="https"
SRC_URL="${SRC_SCHEME}://${SRC_ENDPOINT}"

# ── Destination ───────────────────────────────────────────────────────────────

DEST_REGION="${DEST_STORAGE_REGION:-us-east-1}"
if [[ "$TARGET" == "s3" ]]; then
    DEST_BUCKET="${DEST_STORAGE_BUCKET:-}"
    DEST_AK="${DEST_STORAGE_ACCESS_KEY_ID:-}"
    DEST_SK="${DEST_STORAGE_SECRET_ACCESS_KEY:-}"
    DEST_ENDPOINT_ENV="${DEST_STORAGE_ENDPOINT:-}"
    DEST_SSL="${DEST_STORAGE_USE_SSL:-true}"
    [[ -n "$DEST_BUCKET" && -n "$DEST_AK" && -n "$DEST_SK" ]] \
        || die "--to s3 needs DEST_STORAGE_BUCKET, DEST_STORAGE_ACCESS_KEY_ID and DEST_STORAGE_SECRET_ACCESS_KEY (see --help)."
    DEST_ENDPOINT="${DEST_ENDPOINT_ENV:-s3.amazonaws.com}"
else
    DEST_BUCKET="${DEST_STORAGE_BUCKET:-paca}"
    DEST_AK="${DEST_STORAGE_ACCESS_KEY_ID:-}"
    DEST_SK="${DEST_STORAGE_SECRET_ACCESS_KEY:-}"
    DEST_ENDPOINT_ENV="rustfs:9000"
    DEST_ENDPOINT="rustfs:9000"
    DEST_SSL="false"
    GENERATED_KEYS="no"
    if [[ -z "$DEST_AK" ]]; then DEST_AK="paca$(rand_hex 16)"; GENERATED_KEYS="yes"; fi
    if [[ -z "$DEST_SK" ]]; then DEST_SK="$(rand_hex 40)";     GENERATED_KEYS="yes"; fi
fi
DEST_SCHEME="http"; [[ "$DEST_SSL" == "true" ]] && DEST_SCHEME="https"
DEST_URL="${DEST_SCHEME}://${DEST_ENDPOINT}"

echo ""
echo "  Source      : ${SRC_PROVIDER}  ${SRC_URL}/${SRC_BUCKET}"
echo "  Destination : ${TARGET}  ${DEST_URL}/${DEST_BUCKET}"
echo ""

# ── mc runner ─────────────────────────────────────────────────────────────────
# Credentials travel as environment variables (never on a command line, which
# other users could see in `ps`) and are expanded inside the container.

NETWORK="${PACA_COMPOSE_NETWORK:-}"
BACKUP_ENV=""
API_STOPPED="no"
ENV_SWITCHED="no"

mc_run() {   # mc_run '<sh script>'
    local net_args=()
    [[ -n "$NETWORK" ]] && net_args=(--network "$NETWORK")
    docker run --rm "${net_args[@]}" \
        -e SRC_URL="$SRC_URL" -e SRC_AK="$SRC_AK" -e SRC_SK="$SRC_SK" \
        -e DEST_URL="$DEST_URL" -e DEST_AK="$DEST_AK" -e DEST_SK="$DEST_SK" \
        -e SRC_BUCKET="$SRC_BUCKET" -e DEST_BUCKET="$DEST_BUCKET" -e DEST_REGION="$DEST_REGION" \
        --entrypoint /bin/sh "$MC_IMAGE" -c \
        'mc alias set src "$SRC_URL" "$SRC_AK" "$SRC_SK" >/dev/null && mc alias set dst "$DEST_URL" "$DEST_AK" "$DEST_SK" >/dev/null && '"$1"
}

detect_network() {
    [[ -n "$NETWORK" ]] && return 0
    local svc cid
    for svc in rustfs api; do
        cid="$($COMPOSE_CMD --env-file .env ps -q "$svc" 2>/dev/null | head -1 || true)"
        if [[ -n "$cid" ]]; then
            NETWORK="$(docker inspect -f '{{range $k, $_ := .NetworkSettings.Networks}}{{println $k}}{{end}}' "$cid" | head -1)"
            [[ -n "$NETWORK" ]] && return 0
        fi
    done
    return 1
}

# ── Cleanup / rollback ────────────────────────────────────────────────────────

on_exit() {
    local code=$?
    if [[ $code -ne 0 ]]; then
        echo ""
        error "Migration did not complete. The source bucket was not modified."
        if [[ "$ENV_SWITCHED" == "yes" && -n "$BACKUP_ENV" && -f "$BACKUP_ENV" ]]; then
            cat "$BACKUP_ENV" > .env
            warn "Restored .env from $BACKUP_ENV."
        fi
        if [[ "$API_STOPPED" == "yes" ]]; then
            warn "Restarting api with the original configuration..."
            $COMPOSE_CMD --env-file .env up -d --no-deps api >/dev/null 2>&1 || warn "Could not restart api — run: $COMPOSE_CMD --env-file .env up -d api"
        fi
    fi
}
trap on_exit EXIT

# ── Make both ends reachable ──────────────────────────────────────────────────

heading "Preparing"

if [[ "$TARGET" == "rustfs" ]]; then
    # RustFS reads its credentials from .env at container start, so the new
    # keys must be in .env before it starts. Back up first; on_exit restores.
    BACKUP_ENV=".env.bak.$(date +%s)"
    cp .env "$BACKUP_ENV"
    info "Backed up .env → $BACKUP_ENV"
    if [[ "$DRY_RUN" == "no" ]]; then
        ENV_SWITCHED="yes"
        set_env_var .env STORAGE_ACCESS_KEY_ID "$DEST_AK"
        set_env_var .env STORAGE_SECRET_ACCESS_KEY "$DEST_SK"
        info "Starting the bundled RustFS container..."
        $COMPOSE_CMD --env-file .env up -d --no-deps rustfs >/dev/null
        for _ in $(seq 1 30); do
            [[ "$($COMPOSE_CMD --env-file .env ps rustfs --format '{{.Health}}' 2>/dev/null || true)" == "healthy" ]] && break
            sleep 2
        done
    else
        $COMPOSE_CMD --env-file .env ps -q rustfs 2>/dev/null | grep -q . \
            || warn "Dry run: RustFS isn't running, so only the source listing is shown."
    fi
fi

if ! detect_network; then
    if [[ "$SRC_PROVIDER" == "rustfs" || "$TARGET" == "rustfs" ]]; then
        die "Could not find the compose network (is the stack running?). Set PACA_COMPOSE_NETWORK, e.g. paca_default (see 'docker network ls')."
    fi
    warn "No compose network found; running mc on the default Docker network."
fi
[[ -n "$NETWORK" ]] && info "Docker network: $NETWORK"

info "Checking source credentials..."
mc_run 'mc ls "src/$SRC_BUCKET" >/dev/null' \
    || die "Cannot read source bucket '$SRC_BUCKET' at $SRC_URL — check STORAGE_* in .env."

SRC_STATS="$(mc_run 'mc du --json "src/$SRC_BUCKET"' 2>/dev/null | tail -1 || true)"
SRC_OBJECTS="$(printf '%s' "$SRC_STATS" | sed -n 's/.*"objects":\([0-9]*\).*/\1/p')"
SRC_SIZE="$(printf '%s' "$SRC_STATS"    | sed -n 's/.*"size":\([0-9]*\).*/\1/p')"
info "Source holds ${SRC_OBJECTS:-?} object(s), ${SRC_SIZE:-?} bytes."

if [[ "$DRY_RUN" == "yes" ]]; then
    heading "Dry run"
    mc_run 'mc mirror --dry-run --overwrite "src/$SRC_BUCKET" "dst/$DEST_BUCKET"' || true
    info "Dry run only — nothing was copied or changed."
    exit 0
fi

# ── Quiesce writers ───────────────────────────────────────────────────────────

if $COMPOSE_CMD --env-file .env ps -q api 2>/dev/null | grep -q .; then
    yes_no STOP "Stop the api service during the copy so nothing is uploaded mid-migration? (it is restarted at the end)" "${PACA_STOP_API:-y}"
    if [[ "$STOP" == "yes" ]]; then
        $COMPOSE_CMD --env-file .env stop api >/dev/null
        API_STOPPED="yes"
        info "api stopped."
    else
        warn "api left running: files uploaded during the copy may be missed — re-run this script afterwards to pick them up."
    fi
fi

# ── Copy ──────────────────────────────────────────────────────────────────────

heading "Copying objects"

MB_REGION=""
[[ "$TARGET" == "s3" ]] && MB_REGION='--region "$DEST_REGION"'
mc_run "mc mb --ignore-existing $MB_REGION \"dst/\$DEST_BUCKET\" >/dev/null && mc mirror --overwrite \"src/\$SRC_BUCKET\" \"dst/\$DEST_BUCKET\"" \
    || die "Copy failed (see mc output above)."
info "Copy finished."

# ── Verify ────────────────────────────────────────────────────────────────────

heading "Verifying"

DST_STATS="$(mc_run 'mc du --json "dst/$DEST_BUCKET"' 2>/dev/null | tail -1 || true)"
DST_OBJECTS="$(printf '%s' "$DST_STATS" | sed -n 's/.*"objects":\([0-9]*\).*/\1/p')"
DST_SIZE="$(printf '%s' "$DST_STATS"    | sed -n 's/.*"size":\([0-9]*\).*/\1/p')"
echo "  source      : ${SRC_OBJECTS:-?} objects, ${SRC_SIZE:-?} bytes"
echo "  destination : ${DST_OBJECTS:-?} objects, ${DST_SIZE:-?} bytes"

# The destination may legitimately hold MORE than the source (a pre-existing
# bucket); it must never hold less.
if [[ -z "$SRC_OBJECTS" || -z "$DST_OBJECTS" ]] \
    || (( DST_OBJECTS < SRC_OBJECTS )) || (( ${DST_SIZE:-0} < ${SRC_SIZE:-0} )); then
    die "Verification failed: the destination is missing data. .env was not switched."
fi
info "Destination holds every source object."

# ── Switch .env ───────────────────────────────────────────────────────────────

heading "Switching Paca to ${TARGET}"

yes_no SWITCH "Update .env to use ${TARGET} and restart the api?" "${PACA_SWITCH_ENV:-y}"
if [[ "$SWITCH" != "yes" ]]; then
    [[ "$TARGET" == "rustfs" && -n "$BACKUP_ENV" ]] && { cat "$BACKUP_ENV" > .env; ENV_SWITCHED="no"; }
    warn "Data is copied, but .env is unchanged. Set the STORAGE_* variables yourself (see deploy/.env.production.example)."
    API_STOPPED="no"
    $COMPOSE_CMD --env-file .env up -d --no-deps api >/dev/null 2>&1 || true
    exit 0
fi

if [[ -z "$BACKUP_ENV" ]]; then
    BACKUP_ENV=".env.bak.$(date +%s)"
    cp .env "$BACKUP_ENV"
    info "Backed up .env → $BACKUP_ENV"
fi
ENV_SWITCHED="yes"

set_env_var .env STORAGE_PROVIDER   "$TARGET"
set_env_var .env STORAGE_BUCKET     "$DEST_BUCKET"
set_env_var .env STORAGE_REGION     "$DEST_REGION"
set_env_var .env STORAGE_ACCESS_KEY_ID     "$DEST_AK"
set_env_var .env STORAGE_SECRET_ACCESS_KEY "$DEST_SK"
set_env_var .env STORAGE_USE_SSL    "$DEST_SSL"
if [[ "$TARGET" == "s3" ]]; then
    set_env_var .env STORAGE_ENDPOINT   "$DEST_ENDPOINT_ENV"
    set_env_var .env STORAGE_PUBLIC_URL ""
else
    set_env_var .env STORAGE_ENDPOINT   "rustfs:9000"
    PUB="$(get_env_var .env PUBLIC_URL)"
    set_env_var .env STORAGE_PUBLIC_URL "${PUB%/}/storage"
fi
info ".env updated."

# Recreate the services that read STORAGE_* so they pick the new values up.
# --no-deps: nothing else is touched. The gateway keeps proxying /storage to
# rustfs, which is only used for browser downloads of the bundled store.
$COMPOSE_CMD --env-file .env up -d --no-deps api >/dev/null
API_STOPPED="no"
ENV_SWITCHED="no"   # success — don't roll back on exit
info "api restarted on ${TARGET}."

# ── Next steps ────────────────────────────────────────────────────────────────

heading "Done"
echo "  • Spot-check that existing attachments load in the app."
echo "  • Your previous .env is saved as $BACKUP_ENV; the source bucket is untouched."
if [[ "$TARGET" == "s3" ]]; then
    echo "  • RustFS is no longer used. Future starts should pass --scale rustfs=0"
    echo "    (upgrade.sh does this for you when STORAGE_PROVIDER=s3). Stop it with:"
    echo "        $COMPOSE_CMD --env-file .env stop rustfs"
    echo "    Keep the rustfs_data volume until you're sure, then remove it."
    echo "  • If a bucket CORS policy is needed for browser uploads, configure it on S3."
else
    echo "  • Old S3 bucket was left as-is; delete it yourself once you're satisfied."
    [[ "${GENERATED_KEYS:-no}" == "yes" ]] && echo "  • New RustFS credentials were generated and saved in .env (STORAGE_ACCESS_KEY_ID / STORAGE_SECRET_ACCESS_KEY)."
fi
echo ""

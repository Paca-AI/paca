#!/usr/bin/env bash
# Paca Skills Installer
#
# Installs Paca's bundled skills — plus skills contributed by plugins
# enabled on your Paca instance — into every supported AI coding tool found
# on this machine:
#
#   - Claude Code   → ~/.claude/commands/<name>.md          (global, slash commands)
#   - Gemini CLI    → ~/.gemini/commands/<name>.toml         (global, slash commands)
#   - Cursor        → <project>/.cursor/commands/<name>.md   (project-scoped; Cursor has
#                                                              no global commands directory)
#   - Any AGENTS.md-reading tool (Codex, Windsurf, OpenCode, ...)
#                   → <project>/AGENTS.md                     (project-scoped, merged into
#                                                              a marker-delimited section so
#                                                              any other content is preserved)
#
# The project-scoped targets (Cursor, AGENTS.md) are only written when this
# script is run from inside a git working tree.
#
# Skills are Agent Skills format (YAML frontmatter + markdown body). This
# script strips the frontmatter for Claude Code / Cursor / AGENTS.md, and
# re-shapes it into Gemini CLI's TOML command format.
#
# All skill content — both Paca's bundled defaults and anything contributed
# by an installed plugin — is fetched from a running Paca instance's API
# (GET /api/v1/skills, GET /api/v1/plugins), never from GitHub or any local
# copy, so installed content always matches the exact version that instance
# is running. This means PACA_API_URL is *required*; the script prompts for
# it interactively if not already set via env var (see below), and errors
# clearly if it's never provided.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Paca-AI/paca/master/scripts/install-paca-skills.sh | bash
#   OR (from a local clone):
#   bash scripts/install-paca-skills.sh
#
# Export these first (both picked up automatically — no flags needed):
#   PACA_API_URL=http://localhost:8080 PACA_API_KEY=<key> bash scripts/install-paca-skills.sh
# PACA_API_KEY is optional — both endpoints this script calls are publicly
# readable — but send it if you have one, in case your deployment locks
# things down further. If you don't export PACA_API_URL and are running
# this interactively (i.e. there's a real terminal attached), the script
# prompts for both.
#
# By default, skills are installed to every platform listed above. To
# install to only some of them, either set PACA_SKILL_PLATFORMS to a
# comma/space-separated list of "claude", "gemini", "cursor", "agents"
# (e.g. PACA_SKILL_PLATFORMS=claude,gemini), or pass --platforms=... as an
# argument (works through a pipe too: curl ... | bash -s -- --platforms=claude).
# If neither is set and there's a real terminal attached, the script prompts
# for a selection — press Enter there to install to all of them, matching
# the old default.

set -euo pipefail

REPO="Paca-AI/paca"
BRANCH="master"
CLAUDE_DIR="${HOME}/.claude/commands"
GEMINI_DIR="${HOME}/.gemini/commands"

PACA_API_URL="${PACA_API_URL:-}"
PACA_API_KEY="${PACA_API_KEY:-}"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
NC='\033[0m'

info()    { echo -e "${CYAN}[paca]${NC} $*"; }
success() { echo -e "${GREEN}[paca]${NC} $*"; }
warn()    { echo -e "${YELLOW}[paca]${NC} $*" >&2; }
error()   { echo -e "${RED}[paca] ERROR:${NC} $*" >&2; }

echo ""
echo "  🦙 Paca Skills Installer"
echo "  ────────────────────────────────────"
echo ""

# fetch_optional URL DEST [API_KEY] — best-effort download; returns nonzero
# on failure instead of aborting, so callers decide what's fatal.
#
# The key is sent as X-API-Key, not "Authorization: Bearer" — a Paca API key
# is never a valid JWT, and the API's optional-auth middleware only degrades
# gracefully (silently ignoring a bad/unrecognized credential) for API-key-
# shaped auth. An invalid Bearer token fails JWT verification and 401s
# unconditionally, even on a route that otherwise allows anonymous access —
# so sending it as Bearer would make providing *any* key always fail.
fetch_optional() {
  local url="$1" dest="$2" key="${3:-}"
  if command -v curl &>/dev/null; then
    if [[ -n "$key" ]]; then
      curl -fsSL --max-time 30 -H "X-API-Key: ${key}" "$url" -o "$dest"
    else
      curl -fsSL --max-time 30 "$url" -o "$dest"
    fi
  elif command -v wget &>/dev/null; then
    if [[ -n "$key" ]]; then
      wget -q --timeout=30 --header="X-API-Key: ${key}" -O "$dest" "$url"
    else
      wget -q --timeout=30 -O "$dest" "$url"
    fi
  else
    return 1
  fi
}

# fetch_with_status URL DEST [API_KEY] — like fetch_optional, but also
# records the actual HTTP status code into LAST_FETCH_STATUS ("000" if no
# response was received at all, e.g. connection refused). Used for the two
# Paca API list calls (GET /api/v1/skills, GET /api/v1/plugins): unlike
# every other fetch in this script, those responses tell us whether a
# *provided* PACA_API_KEY was actually rejected (401) — a real problem worth
# stopping for — versus the instance simply being unreachable, which is
# otherwise treated as a soft/hard skip depending on the caller.
LAST_FETCH_STATUS="000"
fetch_with_status() {
  local url="$1" dest="$2" key="${3:-}"
  LAST_FETCH_STATUS="000"
  if command -v curl &>/dev/null; then
    if [[ -n "$key" ]]; then
      LAST_FETCH_STATUS="$(curl -sSL --max-time 30 -o "$dest" -w '%{http_code}' -H "X-API-Key: ${key}" "$url" 2>/dev/null || true)"
    else
      LAST_FETCH_STATUS="$(curl -sSL --max-time 30 -o "$dest" -w '%{http_code}' "$url" 2>/dev/null || true)"
    fi
  elif command -v wget &>/dev/null; then
    local stderr_log
    stderr_log="$(mktemp)"
    if [[ -n "$key" ]]; then
      wget -S --timeout=30 --header="X-API-Key: ${key}" -O "$dest" "$url" 2>"${stderr_log}" || true
    else
      wget -S --timeout=30 -O "$dest" "$url" 2>"${stderr_log}" || true
    fi
    LAST_FETCH_STATUS="$(grep -m1 -oE 'HTTP/[0-9.]+ [0-9]{3}' "${stderr_log}" | awk '{print $2}' || true)"
    [[ -z "${LAST_FETCH_STATUS}" ]] && LAST_FETCH_STATUS="000"
    rm -f "${stderr_log}"
  else
    return 1
  fi
  [[ "${LAST_FETCH_STATUS}" == 2* ]]
}

# to_lower STR — portable lowercasing. `${var,,}` (bash 4+) would do this in
# one step, but the system /bin/bash on macOS is still 3.2 (last GPLv2
# release), and this script is meant to run via `curl | bash` there too.
to_lower() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

# extract_host URL — the bare hostname (no scheme, userinfo, port, or path).
# Handles bracketed IPv6 literals (e.g. "https://[::1]:8080/x" -> "[::1]").
extract_host() {
  local rest="${1#*://}"
  rest="${rest#*@}"
  rest="${rest%%/*}"
  if [[ "${rest}" == \[* ]]; then
    printf '%s' "${rest%%]*}]"
  else
    printf '%s' "${rest%%:*}"
  fi
}

# plugin_baseurl_allowed URL API_HOST — mirrors the same-purpose SSRF guard
# services/ai-agent's resolve_plugin_base_url and apps/mcp/src/plugin-loader.ts's
# resolveImportUrl already apply server-side to this exact field
# (manifest.skills.baseUrl): a plugin manifest is admin-installed but still
# untrusted content, and what it points at here gets installed verbatim as a
# local slash command — so an absolute baseUrl gets the same treatment here,
# not just on the backends. https:// is rejected for loopback/private/
# link-local hosts; http:// is allowed only for localhost/loopback or the
# configured Paca instance's own host. This is a best-effort static check
# (no DNS resolution, unlike the Python guard it mirrors), but it stops the
# obvious case: a plugin declaring an arbitrary external baseUrl.
plugin_baseurl_allowed() {
  local url="$1" api_host="$2"
  local scheme host
  scheme="${url%%://*}"
  host="$(extract_host "${url}")"
  host="$(to_lower "${host}")"

  case "${scheme}" in
    https)
      case "${host}" in
        localhost|127.*|10.*|169.254.*|0.0.0.0|::1|\[::1\])
          return 1 ;;
        172.1[6-9].*|172.2[0-9].*|172.3[01].*)
          return 1 ;;
        192.168.*)
          return 1 ;;
      esac
      return 0
      ;;
    http)
      [[ "${host}" == "localhost" || "${host}" == "127.0.0.1" || "${host}" == "::1" \
        || "${host}" == "[::1]" || "${host}" == "${api_host}" ]]
      ;;
    *)
      return 1
      ;;
  esac
}

if ! command -v curl &>/dev/null && ! command -v wget &>/dev/null; then
  echo "Error: curl or wget is required. Install one and re-run." >&2
  exit 1
fi

# ─── Platform selection ─────────────────────────────────────────────────────

ALL_PLATFORMS="claude gemini cursor agents"
PACA_SKILL_PLATFORMS="${PACA_SKILL_PLATFORMS:-}"

# --platforms=... overrides PACA_SKILL_PLATFORMS — only relevant when this
# script is invoked with args, e.g. `bash scripts/install-paca-skills.sh
# --platforms=claude,gemini` or, through a pipe, `curl ... | bash -s --
# --platforms=claude,gemini` (args after `--` reach the piped script, not
# the outer `bash` invocation).
for arg in "$@"; do
  case "${arg}" in
    --platforms=*) PACA_SKILL_PLATFORMS="${arg#*=}" ;;
  esac
done

# normalize_platforms LIST — validates and echoes a space-separated,
# deduplicated subset of ALL_PLATFORMS; returns nonzero if LIST contains no
# recognized platform at all (an unrecognized token alone, e.g. a typo, is
# warned about rather than treated as fatal — the run still proceeds with
# whatever else was recognized). Accepts "1"-"4" as aliases for
# claude/gemini/cursor/agents too, matching the numbered menu the
# interactive prompt shows — resolved per token here (not by substituting
# digits in the raw string first), so e.g. "1,4" or "1 4" unambiguously
# means claude+agents.
normalize_platforms() {
  local raw="$1" tok matched=false
  local out=""
  for tok in ${raw//,/ }; do
    tok="$(to_lower "${tok}")"
    case "${tok}" in
      all) out="${ALL_PLATFORMS}"; matched=true; continue ;;
      1) tok=claude ;;
      2) tok=gemini ;;
      3) tok=cursor ;;
      4) tok=agents ;;
      claude|gemini|cursor|agents) ;;
      "") continue ;;
      *)
        warn "Unrecognized platform '${tok}' — ignoring it (valid: claude, gemini, cursor, agents, all, or 1-4)"
        continue
        ;;
    esac
    case " ${out} " in
      *" ${tok} "*) ;; # already present — dedup without an associative array
      *) out+="${out:+ }${tok}" ;;
    esac
    matched=true
  done
  $matched || return 1
  printf '%s' "${out}"
}

if [[ -n "${PACA_SKILL_PLATFORMS}" ]]; then
  if ! PACA_SKILL_PLATFORMS="$(normalize_platforms "${PACA_SKILL_PLATFORMS}")"; then
    error "PACA_SKILL_PLATFORMS/--platforms contained no recognized platform (valid: claude, gemini, cursor, agents, all)."
    exit 1
  fi
elif { : < /dev/tty; } 2>/dev/null; then
  echo ""
  info "Which platforms should skills be installed to?"
  info "  1) claude  — Claude Code   (~/.claude/commands/)"
  info "  2) gemini  — Gemini CLI    (~/.gemini/commands/)"
  info "  3) cursor  — Cursor        (project-scoped, needs a git working tree)"
  info "  4) agents  — AGENTS.md     (project-scoped, needs a git working tree)"
  read -r -p "  Enter numbers or names, space/comma-separated (Enter for all): " platform_choice < /dev/tty
  echo ""
  if [[ -z "${platform_choice// /}" ]]; then
    PACA_SKILL_PLATFORMS="${ALL_PLATFORMS}"
  elif ! PACA_SKILL_PLATFORMS="$(normalize_platforms "${platform_choice}")"; then
    error "No recognized platform in that selection — re-run and choose from: claude, gemini, cursor, agents (or 1-4)."
    exit 1
  fi
else
  PACA_SKILL_PLATFORMS="${ALL_PLATFORMS}"
fi

INSTALL_CLAUDE=false
INSTALL_GEMINI=false
INSTALL_CURSOR=false
INSTALL_AGENTS=false
for tok in ${PACA_SKILL_PLATFORMS}; do
  case "${tok}" in
    claude) INSTALL_CLAUDE=true ;;
    gemini) INSTALL_GEMINI=true ;;
    cursor) INSTALL_CURSOR=true ;;
    agents) INSTALL_AGENTS=true ;;
  esac
done
info "Installing to: ${PACA_SKILL_PLATFORMS// /, }"

if $INSTALL_CLAUDE; then mkdir -p "${CLAUDE_DIR}"; fi
if $INSTALL_GEMINI; then mkdir -p "${GEMINI_DIR}"; fi

# Project-scope detection — Cursor has no global commands directory, and
# AGENTS.md is a project-root convention, so both only make sense relative
# to a specific working tree, and only if the user actually selected one of
# them.
PROJECT_ROOT=""
if $INSTALL_CURSOR || $INSTALL_AGENTS; then
  if git rev-parse --is-inside-work-tree &>/dev/null; then
    PROJECT_ROOT="$(git rev-parse --show-toplevel)"
    if $INSTALL_CURSOR; then mkdir -p "${PROJECT_ROOT}/.cursor/commands"; fi
    project_targets="AGENTS.md"
    if $INSTALL_CURSOR && $INSTALL_AGENTS; then
      project_targets="Cursor commands + AGENTS.md"
    elif $INSTALL_CURSOR; then
      project_targets="Cursor commands"
    fi
    info "Project detected (${PROJECT_ROOT}) — also installing ${project_targets} there"
  else
    warn "cursor/agents selected, but not inside a git project — both are project-scoped, so they'll be skipped this run."
  fi
fi

AGENTS_TMP=""
SUMMARY_TMP="$(mktemp)"
if [[ -n "${PROJECT_ROOT}" ]] && $INSTALL_AGENTS; then
  AGENTS_TMP="$(mktemp)"
fi
cleanup() { rm -f "${AGENTS_TMP:-}" "${SUMMARY_TMP}"; }
trap cleanup EXIT

# ─── PACA_API_URL / PACA_API_KEY (if not already set) ──────────────────────

# Every skill this script installs — bundled and plugin-contributed alike —
# comes from a running Paca instance's API, so PACA_API_URL is required.
#
# Piped runs (curl | bash) have stdin consumed by the pipe, not the user's
# keyboard, so read from /dev/tty directly — the classic fix used by
# rustup/nvm-style installers — and only if a controlling terminal actually
# exists. A fully non-interactive run (CI, a Docker build, etc. with no tty
# at all) falls straight through instead of hanging, and fails with a clear
# error a few lines down instead of a `/dev/tty` crash. This has to actually
# attempt to open /dev/tty (redirecting a no-op into it) rather than just
# test `-r /dev/tty` — the device node's permission bits read as readable
# even with no controlling terminal attached, so `-r` alone doesn't catch
# that case.
if [[ -z "${PACA_API_URL}" ]] && { : < /dev/tty; } 2>/dev/null; then
  echo ""
  info "No PACA_API_URL set. This installer needs a running Paca instance to"
  info "fetch skills from."
  read -r -p "  Paca instance URL (e.g. http://localhost:8080): " PACA_API_URL < /dev/tty
  if [[ -n "${PACA_API_URL}" ]]; then
    if [[ -z "${PACA_API_KEY}" ]]; then
      info "API key is optional — both endpoints this script calls are publicly"
      info "readable — but send one in case your deployment locks things down further."
      read -rs -p "  Paca API key (press Enter to skip): " PACA_API_KEY < /dev/tty
      echo ""
    fi
  fi
  echo ""
fi

# ─── Frontmatter helpers ────────────────────────────────────────────────────

strip_frontmatter() {
  awk 'NR==1 && /^---$/{skip=1; next} skip && /^---$/{skip=0; next} !skip' "$1"
}

frontmatter_field() {
  local file="$1" field="$2"
  awk -v field="$field" '
    NR==1 && /^---$/ { infm=1; next }
    infm && /^---$/  { exit }
    infm && index($0, field ":") == 1 {
      sub("^" field ":[ \t]*", "")
      print
      exit
    }
  ' "$file"
}

# TOML basic ("...") strings need backslash/quote escaping; used for the
# short single-line `description` field.
toml_basic_string() {
  printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

# ─── Install one skill into every target ───────────────────────────────────
# $1 = skill name (from the directory / manifest / plugin declaration —
#      authoritative, never re-derived from frontmatter)
# $2 = path to the raw SKILL.md (with frontmatter) on local disk
# $3 = label for logging/summary (e.g. "bundled" or "plugin:com.paca.example")
install_one_skill() {
  local name="$1" raw="$2" label="$3"
  local description body

  description="$(frontmatter_field "${raw}" "description")"
  body="$(strip_frontmatter "${raw}")"

  # Claude Code
  if $INSTALL_CLAUDE; then
    printf '%s\n' "${body}" > "${CLAUDE_DIR}/${name}.md"
  fi

  # Gemini CLI — TOML. `prompt` uses a literal '''...''' multi-line string
  # (zero escaping) since skill bodies routinely contain double quotes (JSON
  # snippets, etc.); `description` uses a basic "..." string since it's a
  # short single line where backslash/quote escaping is cheap and reliable.
  if $INSTALL_GEMINI; then
    if printf '%s' "${body}" | grep -qF "'''"; then
      warn "Skill '${name}' body contains ''' — cannot safely embed as a TOML literal string, skipping Gemini CLI install for it"
    else
      {
        printf 'description = "%s"\n' "$(toml_basic_string "${description}")"
        printf "prompt = '''\n%s\n'''\n" "${body}"
      } > "${GEMINI_DIR}/${name}.toml"
    fi
  fi

  # Cursor — project-scoped only.
  if $INSTALL_CURSOR && [[ -n "${PROJECT_ROOT}" ]]; then
    printf '%s\n' "${body}" > "${PROJECT_ROOT}/.cursor/commands/${name}.md"
  fi

  # AGENTS.md — project-scoped only.
  if $INSTALL_AGENTS && [[ -n "${AGENTS_TMP}" ]]; then
    {
      printf '## /%s\n\n' "${name}"
      [[ -n "${description}" ]] && printf '_%s_\n\n' "${description}"
      printf '%s\n\n' "${body}"
    } >> "${AGENTS_TMP}"
  fi

  printf '  %-22s %s [%s]\n' "/${name}" "${description}" "${label}" >> "${SUMMARY_TMP}"
  success "Installed: ${name} [${label}]"
}

# ─── Bundled skills ─────────────────────────────────────────────────────────

# Bundled skills come from the Paca instance's own API (GET /api/v1/skills),
# not GitHub or a local copy, so the content installed always matches the
# exact version that instance is running. There is nothing else to fall
# back on if it's missing or fails.
if [[ -z "${PACA_API_URL}" ]]; then
  error "PACA_API_URL is required to install skills."
  error "Export it (and optionally PACA_API_KEY), or answer the prompt above, and re-run."
  exit 1
fi
if ! command -v jq &>/dev/null; then
  error "jq is required to install skills from the Paca API — install it and re-run."
  exit 1
fi

info "Fetching bundled skills from ${PACA_API_URL}..."
skills_json="$(mktemp)"
if ! fetch_with_status "${PACA_API_URL%/}/api/v1/skills" "${skills_json}" "${PACA_API_KEY}"; then
  if [[ "${LAST_FETCH_STATUS}" == "401" ]]; then
    error "${PACA_API_URL}/api/v1/skills rejected the request (401 Unauthorized)."
    error "Either PACA_API_KEY is wrong, or this deployment requires authentication that wasn't provided."
    error "Check the key (Paca → Settings → API Keys) and re-run."
  else
    error "Could not fetch skills from ${PACA_API_URL}/api/v1/skills (status: ${LAST_FETCH_STATUS})."
    error "Check the URL is correct and the instance is reachable, then re-run."
  fi
  rm -f "${skills_json}"
  exit 1
fi

bundled_count=0
while IFS= read -r skill_obj; do
  name="$(jq -r '.name' <<<"${skill_obj}")"
  [[ -z "${name}" || "${name}" == "null" ]] && continue
  raw="$(mktemp)"
  jq -r '.content' <<<"${skill_obj}" > "${raw}"
  install_one_skill "${name}" "${raw}" "bundled"
  rm -f "${raw}"
  bundled_count=$((bundled_count + 1))
done < <(jq -c '.data.skills[]' "${skills_json}")
rm -f "${skills_json}"

if [[ "${bundled_count}" -eq 0 ]]; then
  error "${PACA_API_URL} returned no bundled skills — this looks like a bug on that instance, not something to retry."
  exit 1
fi

# ─── Plugin-contributed skills ──────────────────────────────────────────────

# Set whenever the fetch itself doesn't fully succeed (unreachable instance,
# etc.) — printed as a caveat in the final summary instead of letting a
# blanket "Installation complete!" imply everything succeeded. Not fatal:
# bundled skills (above) are already independently complete by this point.
PLUGIN_SKILLS_NOTE=""

# Set only on a 401 from GET /api/v1/plugins — unlike every other failure
# mode here (unreachable instance, missing jq, no plugin skills to install),
# a 401 means the request *did* reach the server and was explicitly
# rejected: either PACA_API_KEY is wrong, or this deployment requires
# authentication that wasn't provided. That's worth stopping for rather than
# quietly degrading like the rest of this script does — checked at the very
# end, after bundled skills (already independently complete by this point)
# get their normal summary, so a bad key doesn't also hide what did work.
FATAL_ERROR=""

# PACA_API_URL, jq, and curl/wget are all already confirmed present by this
# point (checked above for bundled skills, which are never optional) — the
# only thing that can still go wrong here is the /api/v1/plugins call itself.
info "Checking ${PACA_API_URL} for plugin-contributed skills..."
plugins_json="$(mktemp)"
API_HOST="$(to_lower "$(extract_host "${PACA_API_URL}")")"
if fetch_with_status "${PACA_API_URL%/}/api/v1/plugins" "${plugins_json}" "${PACA_API_KEY}"; then
  plugin_skill_count=0
  while IFS=$'\t' read -r plugin_name base_url skill_name; do
    [[ -z "${skill_name}" ]] && continue
    case "${base_url}" in
      http://*|https://*)
        if ! plugin_baseurl_allowed "${base_url}" "${API_HOST}"; then
          warn "Plugin '${plugin_name}' declares skills baseUrl '${base_url}', which resolves to a disallowed host — skipping its skills"
          continue
        fi
        resolved_base="${base_url}"
        ;;
      *) resolved_base="${PACA_API_URL%/}${base_url}" ;;
    esac
    skill_raw="$(mktemp)"
    if fetch_optional "${resolved_base%/}/${skill_name}/SKILL.md" "${skill_raw}"; then
      install_one_skill "${skill_name}" "${skill_raw}" "plugin:${plugin_name}"
      plugin_skill_count=$((plugin_skill_count + 1))
    else
      warn "Failed to fetch skill '${skill_name}' from plugin '${plugin_name}' — skipping it."
    fi
    rm -f "${skill_raw}"
  done < <(jq -r '
    (.data.plugins // [])[]
    | select(.enabled == true)
    | select(.manifest.skills != null)
    | .name as $p
    | (.manifest.skills.baseUrl // "") as $b
    | (.manifest.skills.names // [])[]
    | [$p, $b, .] | @tsv
  ' "${plugins_json}")
  if [[ "${plugin_skill_count}" -eq 0 ]]; then
    info "No plugin-contributed skills found (no enabled plugin declares a skills section)."
  fi
elif [[ "${LAST_FETCH_STATUS}" == "401" ]]; then
  error "${PACA_API_URL}/api/v1/plugins rejected the request (401 Unauthorized)."
  error "Either PACA_API_KEY is wrong, or this deployment requires authentication that wasn't provided."
  error "Check the key (Paca → Settings → API Keys) and re-run."
  FATAL_ERROR="Authentication to ${PACA_API_URL} failed (401) — plugin-contributed skills were not installed."
else
  warn "Could not reach ${PACA_API_URL}/api/v1/plugins — skipping plugin-contributed skills."
  PLUGIN_SKILLS_NOTE="Could not reach ${PACA_API_URL} — plugin-contributed skills were NOT installed, only bundled skills were. Check the URL (and API key, if your deployment needs one) and re-run."
fi
rm -f "${plugins_json}"

# ─── AGENTS.md merge (project-scoped only) ─────────────────────────────────

if [[ -n "${PROJECT_ROOT}" && -n "${AGENTS_TMP}" ]]; then
  agents_file="${PROJECT_ROOT}/AGENTS.md"
  begin_marker="<!-- BEGIN PACA SKILLS (managed by scripts/install-paca-skills.sh — do not edit this section by hand) -->"
  end_marker="<!-- END PACA SKILLS -->"
  block_tmp="$(mktemp)"
  {
    printf '%s\n\n' "${begin_marker}"
    printf '# Paca Skills\n\n'
    printf 'Installed by `scripts/install-paca-skills.sh`. Re-run it to refresh this section.\n\n'
    cat "${AGENTS_TMP}"
    printf '%s\n' "${end_marker}"
  } > "${block_tmp}"

  if [[ -f "${agents_file}" ]] && grep -qF "${begin_marker}" "${agents_file}"; then
    awk -v begin="${begin_marker}" -v end="${end_marker}" -v blockfile="${block_tmp}" '
      $0 == begin { while ((getline line < blockfile) > 0) print line; close(blockfile); skip=1; next }
      $0 == end && skip { skip=0; next }
      skip { next }
      { print }
    ' "${agents_file}" > "${agents_file}.tmp"
    mv "${agents_file}.tmp" "${agents_file}"
  else
    [[ -f "${agents_file}" ]] && printf '\n' >> "${agents_file}"
    cat "${block_tmp}" >> "${agents_file}"
  fi
  rm -f "${block_tmp}"
  success "Updated: ${agents_file}"
fi

# ─── Summary ────────────────────────────────────────────────────────────────

echo ""
if [[ -n "${FATAL_ERROR}" ]]; then
  error "${FATAL_ERROR}"
  echo ""
  warn "Bundled skills below still installed successfully — only the plugin-contributed step failed."
elif [[ -n "${PLUGIN_SKILLS_NOTE}" ]]; then
  warn "${PLUGIN_SKILLS_NOTE}"
  echo ""
  success "Bundled skills installed — see the warning above for what didn't complete."
else
  success "Installation complete!"
fi
echo ""
echo "  Installed skills:"
echo "  ──────────────────────────────────────────────────────────────────────"
# GET /api/v1/skills (the "bundled" fetch above) now also returns plugin-
# contributed skills alongside Paca's own, so a plugin skill gets installed
# twice: once here labeled "bundled" (technically installed, but mislabeled
# — it came from a plugin), then again below labeled "plugin:<name>" (the
# correct label, from resolving it via GET /api/v1/plugins). Both writes are
# identical content to the same file, so nothing is functionally wrong, but
# printing both lines would be confusing. Keep the last (correctly labeled)
# occurrence per skill name while preserving overall order — the classic
# reverse/dedupe-first/reverse idiom, since `awk '!seen[$1]++'` alone would
# keep the first (mislabeled) occurrence instead.
tac "${SUMMARY_TMP}" | awk '!seen[$1]++' | tac
echo ""
echo "  Where they went:"
$INSTALL_CLAUDE && echo "    Claude Code   → ${CLAUDE_DIR}/"
$INSTALL_GEMINI && echo "    Gemini CLI    → ${GEMINI_DIR}/"
if [[ -n "${PROJECT_ROOT}" ]]; then
  $INSTALL_CURSOR && echo "    Cursor        → ${PROJECT_ROOT}/.cursor/commands/"
  $INSTALL_AGENTS && echo "    AGENTS.md     → ${PROJECT_ROOT}/AGENTS.md"
fi
echo ""
echo "  Next step: configure the Paca MCP server (needed for the /paca* commands to work)."
echo "  In a Claude Code session, run:  /paca-setup"
echo ""
echo "  Or add the MCP server manually:"
echo ""
echo "    claude mcp add paca \\"
echo "      --env PACA_API_KEY=<your-api-key> \\"
echo "      --env PACA_API_URL=<your-paca-url> \\"
echo "      -- npx -y @paca-ai/paca-mcp"
echo ""
echo "  Docs: https://github.com/${REPO}/blob/${BRANCH}/docs/guides/install-skills.md"
echo ""

if [[ -n "${FATAL_ERROR}" ]]; then
  exit 1
fi

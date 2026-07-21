#!/usr/bin/env bash
# Paca Skills Installer
#
# Installs Paca's bundled skills — plus, optionally, skills contributed by
# plugins enabled on a specific Paca instance — into every supported AI
# coding tool found on this machine:
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
# Skills are stored in skills/<name>/SKILL.md (Agent Skills format: YAML
# frontmatter + markdown body). This script strips the frontmatter for
# Claude Code / Cursor / AGENTS.md, and re-shapes it into Gemini CLI's TOML
# command format.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Paca-AI/paca/master/scripts/install-paca-skills.sh | bash
#   OR (from a local clone):
#   bash scripts/install-paca-skills.sh
#
# To also install skills contributed by plugins enabled on your Paca
# instance, export these first (both picked up automatically — no flags):
#   PACA_API_URL=http://localhost:8080 PACA_API_KEY=<key> bash scripts/install-paca-skills.sh
# PACA_API_KEY is optional — the plugin list is publicly readable — but send
# it if you have one, in case your deployment locks the endpoint down further.

set -euo pipefail

REPO="Paca-AI/paca"
BRANCH="master"
BASE_URL="https://raw.githubusercontent.com/${REPO}/${BRANCH}/skills"
CLAUDE_DIR="${HOME}/.claude/commands"
GEMINI_DIR="${HOME}/.gemini/commands"

PACA_API_URL="${PACA_API_URL:-}"
PACA_API_KEY="${PACA_API_KEY:-}"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

info()    { echo -e "${CYAN}[paca]${NC} $*"; }
success() { echo -e "${GREEN}[paca]${NC} $*"; }
warn()    { echo -e "${YELLOW}[paca]${NC} $*" >&2; }

echo ""
echo "  🦙 Paca Skills Installer"
echo "  ────────────────────────────────────"
echo ""

# fetch_optional URL DEST [API_KEY] — best-effort download; returns nonzero
# on failure instead of aborting, so callers decide what's fatal.
fetch_optional() {
  local url="$1" dest="$2" key="${3:-}"
  if command -v curl &>/dev/null; then
    if [[ -n "$key" ]]; then
      curl -fsSL -H "Authorization: Bearer ${key}" "$url" -o "$dest"
    else
      curl -fsSL "$url" -o "$dest"
    fi
  elif command -v wget &>/dev/null; then
    if [[ -n "$key" ]]; then
      wget -q --header="Authorization: Bearer ${key}" -O "$dest" "$url"
    else
      wget -q -O "$dest" "$url"
    fi
  else
    return 1
  fi
}

# Detect if running from a local clone (the script lives in scripts/).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || echo "")"
LOCAL_SKILLS=""
if [[ -n "${SCRIPT_DIR}" && -d "${SCRIPT_DIR}/../skills" ]]; then
  LOCAL_SKILLS="${SCRIPT_DIR}/../skills"
  info "Local clone detected — installing bundled skills from ${LOCAL_SKILLS}"
else
  info "Installing bundled skills from GitHub (${REPO}@${BRANCH})"
  if ! command -v curl &>/dev/null && ! command -v wget &>/dev/null; then
    echo "Error: curl or wget is required. Install one and re-run." >&2
    exit 1
  fi
fi

mkdir -p "${CLAUDE_DIR}" "${GEMINI_DIR}"

# Project-scope detection — Cursor has no global commands directory, and
# AGENTS.md is a project-root convention, so both only make sense relative
# to a specific working tree.
PROJECT_ROOT=""
if git rev-parse --is-inside-work-tree &>/dev/null; then
  PROJECT_ROOT="$(git rev-parse --show-toplevel)"
  mkdir -p "${PROJECT_ROOT}/.cursor/commands"
  info "Project detected (${PROJECT_ROOT}) — also installing Cursor commands + AGENTS.md there"
else
  info "Not inside a git project — skipping Cursor commands + AGENTS.md (project-scoped only)"
fi

AGENTS_TMP=""
SUMMARY_TMP="$(mktemp)"
if [[ -n "${PROJECT_ROOT}" ]]; then
  AGENTS_TMP="$(mktemp)"
fi
cleanup() { rm -f "${AGENTS_TMP:-}" "${SUMMARY_TMP}"; }
trap cleanup EXIT

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
  printf '%s\n' "${body}" > "${CLAUDE_DIR}/${name}.md"

  # Gemini CLI — TOML. `prompt` uses a literal '''...''' multi-line string
  # (zero escaping) since skill bodies routinely contain double quotes (JSON
  # snippets, etc.); `description` uses a basic "..." string since it's a
  # short single line where backslash/quote escaping is cheap and reliable.
  if printf '%s' "${body}" | grep -qF "'''"; then
    warn "Skill '${name}' body contains ''' — cannot safely embed as a TOML literal string, skipping Gemini CLI install for it"
  else
    {
      printf 'description = "%s"\n' "$(toml_basic_string "${description}")"
      printf "prompt = '''\n%s\n'''\n" "${body}"
    } > "${GEMINI_DIR}/${name}.toml"
  fi

  # Cursor + AGENTS.md — project-scoped only.
  if [[ -n "${PROJECT_ROOT}" ]]; then
    printf '%s\n' "${body}" > "${PROJECT_ROOT}/.cursor/commands/${name}.md"
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

BUNDLED_NAMES=()
if [[ -n "${LOCAL_SKILLS}" ]]; then
  for d in "${LOCAL_SKILLS}"/*/; do
    [[ -d "${d}" ]] || continue
    BUNDLED_NAMES+=("$(basename "${d}")")
  done
else
  manifest_tmp="$(mktemp)"
  if ! fetch_optional "${BASE_URL}/manifest.txt" "${manifest_tmp}"; then
    echo "Error: failed to download skill manifest from ${BASE_URL}/manifest.txt" >&2
    exit 1
  fi
  while IFS= read -r line || [[ -n "${line}" ]]; do
    case "${line}" in
      ''|'#'*) continue ;;
    esac
    BUNDLED_NAMES+=("${line}")
  done < "${manifest_tmp}"
  rm -f "${manifest_tmp}"
fi

for name in "${BUNDLED_NAMES[@]}"; do
  raw="$(mktemp)"
  if [[ -n "${LOCAL_SKILLS}" ]]; then
    cp "${LOCAL_SKILLS}/${name}/SKILL.md" "${raw}"
  elif ! fetch_optional "${BASE_URL}/${name}/SKILL.md" "${raw}"; then
    echo "Error: failed to download ${BASE_URL}/${name}/SKILL.md" >&2
    exit 1
  fi
  install_one_skill "${name}" "${raw}" "bundled"
  rm -f "${raw}"
done

# ─── Plugin-contributed skills (optional) ──────────────────────────────────

if [[ -z "${PACA_API_URL}" ]]; then
  info "PACA_API_URL not set — skipping plugin-contributed skills."
  info "Export PACA_API_URL (and optionally PACA_API_KEY) and re-run to also install skills from plugins enabled on your Paca instance."
elif ! command -v jq &>/dev/null; then
  warn "jq is not installed — skipping plugin-contributed skills (install jq to enable this)."
elif ! command -v curl &>/dev/null && ! command -v wget &>/dev/null; then
  warn "curl or wget is required to fetch plugin skills — skipping."
else
  info "Checking ${PACA_API_URL} for plugin-contributed skills..."
  plugins_json="$(mktemp)"
  if fetch_optional "${PACA_API_URL%/}/api/v1/plugins" "${plugins_json}" "${PACA_API_KEY}"; then
    plugin_skill_count=0
    while IFS=$'\t' read -r plugin_name base_url skill_name; do
      [[ -z "${skill_name}" ]] && continue
      case "${base_url}" in
        http://*|https://*) resolved_base="${base_url}" ;;
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
      (.plugins // [])[]
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
  else
    warn "Could not reach ${PACA_API_URL}/api/v1/plugins — skipping plugin-contributed skills."
  fi
  rm -f "${plugins_json}"
fi

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
success "Installation complete!"
echo ""
echo "  Installed skills:"
echo "  ──────────────────────────────────────────────────────────────────────"
cat "${SUMMARY_TMP}"
echo ""
echo "  Where they went:"
echo "    Claude Code   → ${CLAUDE_DIR}/"
echo "    Gemini CLI    → ${GEMINI_DIR}/"
if [[ -n "${PROJECT_ROOT}" ]]; then
  echo "    Cursor        → ${PROJECT_ROOT}/.cursor/commands/"
  echo "    AGENTS.md     → ${PROJECT_ROOT}/AGENTS.md"
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
if [[ -z "${PACA_API_URL}" ]]; then
  echo "  To also install skills from plugins enabled on your instance next time:"
  echo "    PACA_API_URL=<your-paca-url> PACA_API_KEY=<your-api-key> bash $0"
  echo ""
fi
echo "  Docs: https://github.com/${REPO}/blob/${BRANCH}/docs/guides/install-skills.md"
echo ""

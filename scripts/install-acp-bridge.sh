#!/usr/bin/env bash
# paca-acp-bridge – installs the prebuilt ACP bridge binary
#
# Downloads the release archive matching this machine's OS/arch, extracts
# it, and drops `paca-acp-bridge` on your PATH. No Python, uv, or Go
# toolchain required — only this script and (separately, to actually run
# one of the built-in providers) Node.js for the `npx`-launched ACP CLI.
#
#   curl -fsSL https://github.com/Paca-AI/paca/releases/latest/download/install-acp-bridge.sh | bash
#
# Environment variables (all optional):
#   PACA_ACP_BRIDGE_VERSION   Release tag to install (default: latest)
#   PACA_ACP_BRIDGE_INSTALL_DIR   Directory to install into (default: ~/.local/bin)

set -euo pipefail

BOLD='\033[1m'; RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
RESET='\033[0m'

info()  { echo -e "${GREEN}✔${RESET}  $*"; }
warn()  { echo -e "${YELLOW}!${RESET}  $*"; }
error() { echo -e "${RED}✖${RESET}  $*" >&2; }
die()   { error "$*"; exit 1; }
bold()  { echo -e "${BOLD}$*${RESET}"; }

# ── Version / URL resolution ──────────────────────────────────────────────────

# CD stamps this to the exact tag of the release install-acp-bridge.sh ships
# with (see the "Prepare assets" step in .github/workflows/cd.yml), so a
# plain `bash install-acp-bridge.sh` installs a version that's guaranteed to
# exist instead of whatever :latest happens to resolve to. The source tree
# keeps "latest" so a checkout run directly still behaves sensibly. Keep this
# a standalone `NAME="value"` assignment — CD's sed matches on that exact
# shape (see scripts/install.sh's identical convention).
PACA_DEFAULT_VERSION="latest"

VERSION="${PACA_ACP_BRIDGE_VERSION:-$PACA_DEFAULT_VERSION}"
INSTALL_DIR="${PACA_ACP_BRIDGE_INSTALL_DIR:-$HOME/.local/bin}"

if [[ "$VERSION" == "latest" ]]; then
    RELEASE_BASE="https://github.com/Paca-AI/paca/releases/latest/download"
else
    RELEASE_BASE="https://github.com/Paca-AI/paca/releases/download/${VERSION}"
fi

# ── OS/arch detection ──────────────────────────────────────────────────────────

case "$(uname -s)" in
    Linux)  GOOS=linux ;;
    Darwin) GOOS=darwin ;;
    *)      die "Unsupported OS: $(uname -s). Download a binary manually from https://github.com/Paca-AI/paca/releases/latest — Windows users want the .zip archive." ;;
esac

case "$(uname -m)" in
    x86_64|amd64)  GOARCH=amd64 ;;
    arm64|aarch64) GOARCH=arm64 ;;
    *)             die "Unsupported architecture: $(uname -m)" ;;
esac

ARCHIVE="paca-acp-bridge_${VERSION#v}_${GOOS}_${GOARCH}.tar.gz"
URL="${RELEASE_BASE}/${ARCHIVE}"

# ── Download and install ────────────────────────────────────────────────────────

bold "Installing paca-acp-bridge (${GOOS}/${GOARCH}, ${VERSION})"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

if ! curl -fsSL "$URL" -o "$TMP_DIR/$ARCHIVE"; then
    die "Failed to download $URL — check that $VERSION is a real release tag."
fi
tar -xzf "$TMP_DIR/$ARCHIVE" -C "$TMP_DIR"

mkdir -p "$INSTALL_DIR"
mv "$TMP_DIR/paca-acp-bridge" "$INSTALL_DIR/paca-acp-bridge"
chmod +x "$INSTALL_DIR/paca-acp-bridge"

info "Installed to $INSTALL_DIR/paca-acp-bridge"

case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *)
        warn "$INSTALL_DIR is not on your PATH."
        echo "  Add this to your shell profile:"
        echo "    export PATH=\"$INSTALL_DIR:\$PATH\""
        ;;
esac

if command -v node &>/dev/null; then
    info "Node.js found ($(node --version)) — the built-in providers (Claude Code / Codex / Gemini CLI) are npx-launched and need it."
else
    warn "Node.js not found — install it before running the bridge against a built-in provider (Claude Code / Codex / Gemini CLI). Goose or a custom ACP server may not need it."
fi

echo ""
bold "Next: copy the run command from Paca's Agents UI (or see apps/acp-bridge/README.md), e.g."
echo "  $INSTALL_DIR/paca-acp-bridge run --agent-id <id> --token <token> --server <your-paca-url>"

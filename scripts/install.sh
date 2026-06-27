#!/usr/bin/env sh
# install.sh — download and install the mew binary for the current platform.
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/kash04x/mew/main/scripts/install.sh | sh
#
# Or locally after cloning:
#   ./scripts/install.sh

set -e

# ── Configuration ─────────────────────────────────────────────────────────────
REPO="kash04x/mew"
BINARY="mew"
INSTALL_DIR="/usr/local/bin"
# ──────────────────────────────────────────────────────────────────────────────

log()  { printf '  %s\n' "$*"; }
err()  { printf 'Error: %s\n' "$*" >&2; exit 1; }
bold() { printf '\033[1m%s\033[0m\n' "$*"; }

detect_os() {
    case "$(uname -s)" in
        Darwin) echo "darwin" ;;
        Linux)  echo "linux"  ;;
        *)      err "Unsupported OS: $(uname -s)" ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64)          echo "amd64" ;;
        arm64 | aarch64) echo "arm64" ;;
        *)               err "Unsupported architecture: $(uname -m)" ;;
    esac
}

OS=$(detect_os)
ARCH=$(detect_arch)
ASSET="${BINARY}-${OS}-${ARCH}"

bold "Installing mew"
log "Platform : ${OS}/${ARCH}"
log "Asset    : ${ASSET}"

log "Fetching latest release from github.com/${REPO}..."
DOWNLOAD_URL=$(curl -sSf "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"browser_download_url"' \
    | grep "${ASSET}" \
    | sed 's/.*"browser_download_url": *"\([^"]*\)".*/\1/')

if [ -z "$DOWNLOAD_URL" ]; then
    err "No release asset found for ${ASSET}. Check https://github.com/${REPO}/releases"
fi
log "URL      : ${DOWNLOAD_URL}"

TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT

log "Downloading..."
curl -sSfL "$DOWNLOAD_URL" -o "$TMP"
chmod +x "$TMP"

if [ -w "$INSTALL_DIR" ]; then
    mv "$TMP" "${INSTALL_DIR}/${BINARY}"
else
    log "No write access to ${INSTALL_DIR} — trying with sudo..."
    sudo mv "$TMP" "${INSTALL_DIR}/${BINARY}"
fi

bold ""
bold "Done!"
log "Installed : $(command -v ${BINARY})"
log "Version   : $("${INSTALL_DIR}/${BINARY}" version 2>/dev/null || echo 'run mew version')"
log ""
log "Next steps:"
log "  mew config init     # add your Redash / ClickUp credentials"
log "  mew install         # register with Claude Code"
log "  mew doctor          # verify everything works"

#!/bin/sh
# Shovels CLI installer
# Usage: curl -LsSf https://shovels.ai/install.sh | sh
#
# Environment variables:
#   SHOVELS_VERSION     Pin to a specific version (e.g. v0.1.0)
#   SHOVELS_INSTALL_DIR Override install directory (default: ~/.shovels/bin)
#   GITHUB_TOKEN        Required while repo is private; not needed once public
set -eu

REPO="ShovelsAI/shovels-cli"
INSTALL_DIR="${SHOVELS_INSTALL_DIR:-$HOME/.shovels/bin}"

# --- helpers ----------------------------------------------------------------

say() { printf '%s\n' "$*"; }
err() { say "Error: $*" >&2; exit 1; }

need() {
  command -v "$1" >/dev/null 2>&1 || err "'$1' is required but not found"
}

# Authenticated curl — passes GITHUB_TOKEN if set
fetch() {
  if [ -n "${GITHUB_TOKEN:-}" ]; then
    curl -fsSL --retry 3 -H "Authorization: Bearer ${GITHUB_TOKEN}" "$@"
  else
    curl -fsSL --retry 3 "$@"
  fi
}

# --- detect platform --------------------------------------------------------

detect_os() {
  case "$(uname -s)" in
    Linux*)  echo "linux" ;;
    Darwin*) echo "darwin" ;;
    MINGW*|MSYS*|CYGWIN*) echo "windows" ;;
    *) err "Unsupported OS: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)   echo "amd64" ;;
    aarch64|arm64)   echo "arm64" ;;
    *) err "Unsupported architecture: $(uname -m)" ;;
  esac
}

# --- resolve latest version -------------------------------------------------

latest_version() {
  fetch "https://api.github.com/repos/${REPO}/releases/latest" |
    grep '"tag_name"' | cut -d'"' -f4
}

# --- download release assets ------------------------------------------------

# For private repos, github.com download URLs 404 even with a token.
# We must use the API to find asset IDs, then download via the API.

download_release_assets() {
  RELEASE_TAG="$1"
  shift
  # remaining args are filename/dest pairs

  if [ -z "${GITHUB_TOKEN:-}" ]; then
    # Public repo: direct download
    BASE="https://github.com/${REPO}/releases/download/${RELEASE_TAG}"
    while [ $# -ge 2 ]; do
      fetch "${BASE}/$1" -o "$2"
      shift 2
    done
    return
  fi

  # Private repo: query API for asset list, download by ID
  ASSETS_JSON=$(fetch "https://api.github.com/repos/${REPO}/releases/tags/${RELEASE_TAG}")

  while [ $# -ge 2 ]; do
    FILENAME="$1"
    DEST="$2"
    shift 2

    ASSET_URL=$(printf '%s' "$ASSETS_JSON" | grep -B2 "\"name\": \"${FILENAME}\"" | grep '"url"' | cut -d'"' -f4)
    if [ -z "$ASSET_URL" ]; then
      err "Asset '${FILENAME}' not found in release ${RELEASE_TAG}"
    fi
    fetch -H "Accept: application/octet-stream" "${ASSET_URL}" -o "${DEST}"
  done
}

# --- main -------------------------------------------------------------------

main() {
  need curl
  need tar

  OS=$(detect_os)
  ARCH=$(detect_arch)

  say "Resolving latest version..."
  VERSION="${SHOVELS_VERSION:-$(latest_version)}"
  if [ -z "$VERSION" ]; then
    err "Could not determine latest version. If the repo is private, set GITHUB_TOKEN."
  fi
  VERSION_NUM="${VERSION#v}"  # strip leading v

  say "Installing shovels ${VERSION} (${OS}/${ARCH})"

  # Build archive name
  if [ "$OS" = "windows" ]; then
    ARCHIVE="shovels_${VERSION_NUM}_${OS}_${ARCH}.zip"
  else
    ARCHIVE="shovels_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"
  fi
  BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"

  # Download to temp dir
  TEMP_DIR=$(mktemp -d)
  trap 'rm -rf "$TEMP_DIR"' EXIT

  say "Downloading ${ARCHIVE}..."
  download_release_assets "${VERSION}" \
    "${ARCHIVE}" "${TEMP_DIR}/${ARCHIVE}" \
    "checksums.txt" "${TEMP_DIR}/checksums.txt"

  # Verify checksum
  say "Verifying checksum..."
  EXPECTED=$(grep "${ARCHIVE}" "${TEMP_DIR}/checksums.txt" | cut -d' ' -f1)
  if [ -z "$EXPECTED" ]; then
    err "Archive not found in checksums.txt"
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    ACTUAL=$(sha256sum "${TEMP_DIR}/${ARCHIVE}" | cut -d' ' -f1)
  elif command -v shasum >/dev/null 2>&1; then
    ACTUAL=$(shasum -a 256 "${TEMP_DIR}/${ARCHIVE}" | cut -d' ' -f1)
  else
    say "Warning: no sha256sum or shasum found, skipping verification"
    ACTUAL="$EXPECTED"
  fi

  if [ "$EXPECTED" != "$ACTUAL" ]; then
    err "Checksum mismatch: expected ${EXPECTED}, got ${ACTUAL}"
  fi

  # Extract
  if [ "$OS" = "windows" ]; then
    need unzip
    unzip -qo "${TEMP_DIR}/${ARCHIVE}" -d "${TEMP_DIR}"
  else
    tar -xzf "${TEMP_DIR}/${ARCHIVE}" -C "${TEMP_DIR}"
  fi

  # Install
  mkdir -p "${INSTALL_DIR}"
  mv "${TEMP_DIR}/shovels" "${INSTALL_DIR}/shovels"
  chmod +x "${INSTALL_DIR}/shovels"

  say "Installed shovels to ${INSTALL_DIR}/shovels"

  # Add to PATH if needed
  add_to_path

  say ""
  say "Run 'shovels version' to verify."
  say "Run 'shovels --help' to get started."
}

# --- PATH setup -------------------------------------------------------------

add_to_path() {
  # Already on PATH?
  case ":${PATH}:" in
    *":${INSTALL_DIR}:"*) return ;;
  esac

  SHELL_NAME=$(basename "${SHELL:-/bin/sh}")
  PROFILE=""

  case "$SHELL_NAME" in
    zsh)  PROFILE="$HOME/.zshrc" ;;
    bash)
      if [ -f "$HOME/.bashrc" ]; then
        PROFILE="$HOME/.bashrc"
      else
        PROFILE="$HOME/.bash_profile"
      fi
      ;;
    fish)
      FISH_DIR="$HOME/.config/fish/conf.d"
      mkdir -p "$FISH_DIR"
      if ! grep -q "shovels" "$FISH_DIR/shovels.fish" 2>/dev/null; then
        printf 'set -gx PATH "%s" $PATH\n' "$INSTALL_DIR" > "$FISH_DIR/shovels.fish"
        say "Added ${INSTALL_DIR} to PATH in ${FISH_DIR}/shovels.fish"
      fi
      return
      ;;
    *)    PROFILE="$HOME/.profile" ;;
  esac

  if [ -n "$PROFILE" ]; then
    if ! grep -q "/.shovels/bin" "$PROFILE" 2>/dev/null; then
      printf '\n# Shovels CLI\nexport PATH="%s:$PATH"\n' "$INSTALL_DIR" >> "$PROFILE"
      say "Added ${INSTALL_DIR} to PATH in ${PROFILE}"
      say "Restart your shell or run: source ${PROFILE}"
    fi
  fi
}

main "$@"

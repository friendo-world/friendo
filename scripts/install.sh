#!/bin/sh
# Install the latest `friendo` release binary.
#
#   curl -fsSL https://raw.githubusercontent.com/friendo-world/friendo/main/scripts/install.sh | sh
#
# Downloads the right prebuilt archive from GitHub Releases and installs `friendo`
# to a bin dir on your PATH (default: /usr/local/bin, or ~/.local/bin without sudo).
# Override with: FRIENDO_INSTALL_DIR=/somewhere sh install.sh
set -eu

REPO="friendo-world/friendo"
BIN="friendo"

# --- detect os/arch (matching GoReleaser's name_template) ---
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch="amd64" ;;
  arm64 | aarch64) arch="arm64" ;;
  *) echo "friendo: unsupported architecture: $arch" >&2; exit 1 ;;
esac
case "$os" in
  darwin | linux) ext="tar.gz" ;;
  *) echo "friendo: unsupported OS: $os (Windows: download the .zip from the Releases page)" >&2; exit 1 ;;
esac

# --- resolve latest version tag ---
if [ -n "${FRIENDO_VERSION:-}" ]; then
  tag="$FRIENDO_VERSION"
else
  tag=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
fi
if [ -z "${tag:-}" ]; then
  echo "friendo: could not determine the latest release. Is the repo published with a release yet?" >&2
  exit 1
fi
version=${tag#v}

asset="${BIN}_${version}_${os}_${arch}.${ext}"
url="https://github.com/$REPO/releases/download/$tag/$asset"

# --- choose an install dir on PATH ---
if [ -n "${FRIENDO_INSTALL_DIR:-}" ]; then
  dir="$FRIENDO_INSTALL_DIR"
elif [ -w /usr/local/bin ] 2>/dev/null; then
  dir="/usr/local/bin"
else
  dir="$HOME/.local/bin"
fi
mkdir -p "$dir"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $BIN $tag ($os/$arch)..."
curl -fsSL "$url" -o "$tmp/pkg.$ext" || {
  echo "friendo: download failed: $url" >&2; exit 1;
}
tar -xzf "$tmp/pkg.$ext" -C "$tmp"
install -m 0755 "$tmp/$BIN" "$dir/$BIN" 2>/dev/null || { cp "$tmp/$BIN" "$dir/$BIN"; chmod 0755 "$dir/$BIN"; }

echo "Installed $BIN to $dir/$BIN"
case ":$PATH:" in
  *":$dir:"*) : ;;
  *) echo "Note: $dir is not on your PATH. Add it with:"; echo "  export PATH=\"$dir:\$PATH\"" ;;
esac
"$dir/$BIN" --version 2>/dev/null || true

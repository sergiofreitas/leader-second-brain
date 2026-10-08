#!/bin/sh
# Second Brain installer for macOS and Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/sergiofreitas/leader-second-brain/main/install.sh | sh
#
# Downloads the second-brain binary from GitHub Releases, verifies its
# SHA-256 against the release's checksums.txt and installs it in
# ~/.second-brain/bin.
#
# Options (environment variables):
#   SECOND_BRAIN_VERSION       release tag to install (default: the latest)
#   SECOND_BRAIN_INSTALL_DIR   install folder (default: ~/.second-brain/bin)
#   SECOND_BRAIN_BASE_URL      download from this mirror of the release files
#   SECOND_BRAIN_BINARY        install this local file instead of downloading
#                              (for testing a build before releasing it)
set -eu

REPO="sergiofreitas/leader-second-brain"
INSTALL_DIR="${SECOND_BRAIN_INSTALL_DIR:-$HOME/.second-brain/bin}"
TARGET="$INSTALL_DIR/second-brain"

fail() { echo "error: $*" >&2; exit 1; }

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) fail "unsupported system $(uname -s) (on Windows, use install.ps1)" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) fail "unsupported architecture $(uname -m)" ;;
esac
asset="second-brain-$os-$arch"

download() { # url file
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -q "$1" -O "$2"
  else
    fail "curl or wget is required"
  fi
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | cut -d' ' -f1
  else
    fail "sha256sum or shasum is required to verify the download"
  fi
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

if [ -n "${SECOND_BRAIN_BINARY:-}" ]; then
  echo "Installing the local binary $SECOND_BRAIN_BINARY (no download, no checksum)"
  cp "$SECOND_BRAIN_BINARY" "$tmp/$asset"
else
  if [ -n "${SECOND_BRAIN_BASE_URL:-}" ]; then
    base="${SECOND_BRAIN_BASE_URL%/}"
  elif [ -n "${SECOND_BRAIN_VERSION:-}" ]; then
    base="https://github.com/$REPO/releases/download/$SECOND_BRAIN_VERSION"
  else
    base="https://github.com/$REPO/releases/latest/download"
  fi
  echo "Downloading $asset from $base"
  download "$base/$asset" "$tmp/$asset"
  download "$base/checksums.txt" "$tmp/checksums.txt"

  expected="$(awk -v a="$asset" '$2 == a || $2 == "*"a { print $1 }' "$tmp/checksums.txt")"
  [ -n "$expected" ] || fail "checksums.txt has no entry for $asset"
  actual="$(sha256 "$tmp/$asset")"
  [ "$expected" = "$actual" ] || fail "checksum mismatch for $asset (expected $expected, got $actual); not installing"
  echo "Checksum verified."
fi

mkdir -p "$INSTALL_DIR"
chmod +x "$tmp/$asset"
mv -f "$tmp/$asset" "$TARGET"

echo
echo "Installed $("$TARGET" version) at $TARGET"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    echo
    echo "Add it to your PATH (MCP hosts start it as \"second-brain\"):"
    echo "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.zshrc   # or ~/.bashrc"
    ;;
esac

echo
echo "Next steps:"
echo "  1. Create a config (optional; without one, search is by keyword only):"
echo "       second-brain init --help"
echo "  2. Install the Claude Code plugin:"
echo "       claude plugin marketplace add $REPO"
echo "       claude plugin install second-brain@second-brain"
echo "  3. Restart Claude Code."

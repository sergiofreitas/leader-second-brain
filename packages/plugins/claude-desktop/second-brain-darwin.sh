#!/bin/sh
# Starts the macOS binary for this Mac's architecture. The extension ships
# both, since manifest platform overrides can't tell Apple Silicon from Intel.
dir=$(dirname "$0")
case "$(uname -m)" in
  arm64) bin="$dir/second-brain-darwin-arm64" ;;
  *) bin="$dir/second-brain-darwin-amd64" ;;
esac
# The bundle may not keep the executable bit
[ -x "$bin" ] || chmod +x "$bin"
exec "$bin" "$@"

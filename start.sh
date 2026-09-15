#!/bin/sh
set -eu
cd "$(dirname "$0")"
export HOST="${HOST:-0.0.0.0}" PORT="${PORT:-8000}" GOMAXPROCS="${GOMAXPROCS:-1}"
export SNAKE_CONFIG="$PWD/policy.json"
case "$(uname -s)-$(uname -m)" in
  Linux-x86_64) binary=bin/abraham-linux-amd64 ;;
  Linux-aarch64|Linux-arm64) binary=bin/abraham-linux-arm64 ;;
  Darwin-arm64) binary=bin/abraham-darwin-arm64 ;;
  *) echo 'Build this source with Go 1.26+ for your platform.' >&2; exit 1 ;;
esac
chmod +x "$binary"
exec "./$binary"

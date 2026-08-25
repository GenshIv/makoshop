#!/usr/bin/env bash
#
# build.sh — production build for Makoshop.
#
# Builds:
#   1. The frontend (frontend/dist) — served by the Go server.
#   2. The backend binary (./makoshop) — self-contained via vendor/.
#
# Usage:
#   ./build.sh
#
# The resulting ./makoshop binary plus frontend/dist/ are all that is needed
# to run the server on another machine (no Go module cache, no local replaces).
set -euo pipefail

cd "$(dirname "$0")"

# Ensure a writable Go build cache. Use an existing GOCACHE if set; otherwise
# fall back to a project-local directory so the build works on any machine.
if [ -z "${GOCACHE:-}" ]; then
  export GOCACHE="$(pwd)/.gocache"
  mkdir -p "$GOCACHE"
fi

echo "=== [1/2] Building frontend ==="
cd frontend
if [ ! -d node_modules ]; then
  echo "node_modules not found, running npm install..."
  npm install
fi
npm run build
cd ..

echo "=== [2/2] Building backend (vendored) ==="
# -mod=vendor uses the vendored dependencies, so this works on any machine
# without the local makodb/silentjson checkouts.
CGO_ENABLED=0 go build -mod=vendor -trimpath -ldflags="-s -w" -o makoshop ./cmd/server/

echo ""
echo "Build complete:"
echo "  - makoshop        (server binary)"
echo "  - frontend/dist/  (static frontend, served by the binary)"
echo ""
echo "Deploy both to the production server, then run: ./makoshop"

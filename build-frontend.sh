#!/usr/bin/env bash
# Rebuilds the shared Vue SPA (repo-root frontend/) into the Wails embed dir.
# Usage: bash build-frontend.sh  (run from this directory, or via wails build)
set -euo pipefail
cd "$(dirname "$0")"

ROOT="$(cd .. && pwd)"

if [ ! -d "$ROOT/frontend/node_modules" ]; then
  npm ci --prefix "$ROOT/frontend"
fi
npm run build --prefix "$ROOT/frontend"

rm -rf frontend/dist
mkdir -p frontend/dist
cp -r "$ROOT/frontend/dist/"* frontend/dist/
cp -r "$ROOT/public/tinymce" frontend/dist/tinymce
echo "frontend/dist updated: $(du -sh frontend/dist | cut -f1)"

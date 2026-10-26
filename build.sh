#!/usr/bin/env bash
# Builds ./swarm from src/, with src/web and src/samples packed in. "./build.sh install" also copies it to ~/.local/bin.
set -euo pipefail
root="$(cd "$(dirname "$0")" && pwd)"
cd "$root/src"

if command -v node >/dev/null; then
  node --check web/app.js
fi
gofmt -l . | { ! grep .; } || { echo "run gofmt -w src first" >&2; exit 1; }
go vet ./...
go build -o "$root/swarm" .
echo "built $root/swarm"

if [ "${1:-}" = "install" ]; then
  mkdir -p ~/.local/bin
  cp "$root/swarm" ~/.local/bin/swarm
  echo "installed ~/.local/bin/swarm"
fi

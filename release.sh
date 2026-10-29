#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

part="${1:-patch}"
case "$part" in
  major | minor | patch) ;;
  *)
    echo "usage: ./release.sh [major|minor|patch]" >&2
    exit 1
    ;;
esac

if [ -n "$(git status --porcelain)" ]; then
  echo "release.sh: commit or stash your changes first" >&2
  exit 1
fi

git fetch --quiet --tags origin
last=$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | head -n 1)
IFS=. read -r major minor patch <<< "${last:-v0.0.0}"
major="${major#v}"

case "$part" in
  major) next="v$((major + 1)).0.0" ;;
  minor) next="v$major.$((minor + 1)).0" ;;
  patch) next="v$major.$minor.$((patch + 1))" ;;
esac

git tag -a "$next" -m "$next"
git push --quiet origin "$next"
echo "pushed $next (previous: ${last:-none}); the release workflow builds it"

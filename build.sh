#!/usr/bin/env bash
set -uo pipefail
root="$(cd "$(dirname "$0")" && pwd)"

min_coverage=80

check_comments() {
  ast-grep scan -r "$root/lint/no-comments.yml" "$root/src" "$root/test"
}

check_lengths() {
  local bad=0 file lines
  while IFS= read -r file; do
    lines=$(awk 'END { print NR }' "$root/$file")
    if [ "$lines" -gt 400 ]; then
      echo "$file has $lines lines, the limit is 400"
      bad=1
    fi
  done < <(cd "$root" && find src \( -name '*.go' -o -path 'src/web/*.js' -o -path 'src/web/*.css' -o -path 'src/web/*.html' \) -type f | sort)
  return "$bad"
}

check_gofmt() {
  local unformatted
  unformatted=$(cd "$root/src" && gofmt -l .)
  if [ -n "$unformatted" ]; then
    echo "$unformatted"
    echo "run gofmt -w src first"
    return 1
  fi
}

check_vet() {
  cd "$root/src" && go vet ./...
}

check_lint() {
  if ! command -v golangci-lint >/dev/null; then
    echo "golangci-lint is not installed: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.3" >&2
    return 1
  fi
  cd "$root/src" && golangci-lint run ./...
}

check_tests() {
  cd "$root/src" || return 1
  local profile race=() total
  profile=$(mktemp)
  if [ "$(go env CGO_ENABLED)" = 1 ] && command -v gcc >/dev/null; then
    race=(-race)
  fi
  if ! go test "${race[@]}" -count=1 -coverprofile="$profile" ./...; then
    rm -f "$profile"
    return 1
  fi
  total=$(go tool cover -func="$profile" | awk '/^total:/ { sub("%", "", $NF); print $NF }')
  rm -f "$profile"
  echo "go coverage: $total% (minimum $min_coverage%)"
  awk -v total="$total" -v min="$min_coverage" 'BEGIN { exit !(total >= min) }'
}

check_web_tests() {
  if [ ! -d "$root/node_modules/happy-dom" ]; then
    echo "run npm install in $root first" >&2
    return 1
  fi
  cd "$root" && node --test --experimental-test-coverage --test-coverage-include='src/web/*.js' \
    --test-coverage-lines="$min_coverage" 'test/web/*.test.mjs'
}

check_eslint() {
  if [ ! -x "$root/node_modules/.bin/eslint" ]; then
    echo "run npm install in $root first" >&2
    return 1
  fi
  cd "$root" && node_modules/.bin/eslint src/web test
}

check_js_syntax() {
  local bad=0 file
  if ! command -v node >/dev/null; then
    echo "node is required" >&2
    return 1
  fi
  for file in "$root"/src/web/*.js; do
    node --check "$file" || bad=1
  done
  return "$bad"
}

build() {
  cd "$root/src" && go build -o "$root/swarm" .
}

failed=()
run() {
  local name="$1"
  shift
  echo "== $name"
  if "$@"; then
    echo "ok: $name"
  else
    failed+=("$name")
  fi
}

run "comment check" check_comments
run "line limit" check_lengths
run "gofmt" check_gofmt
run "go vet" check_vet
run "golangci-lint" check_lint
run "go test" check_tests
run "web test" check_web_tests
run "eslint" check_eslint
run "node --check" check_js_syntax

if [ "${#failed[@]}" -gt 0 ]; then
  echo "build.sh: failed checks: ${failed[*]}" >&2
  exit 1
fi

if ! build; then
  echo "build.sh: go build failed" >&2
  exit 1
fi
echo "built $root/swarm"

if [ "${1:-}" = "install" ]; then
  mkdir -p ~/.local/bin
  if ! { cp "$root/swarm" ~/.local/bin/.swarm.new && mv -f ~/.local/bin/.swarm.new ~/.local/bin/swarm; }; then
    rm -f ~/.local/bin/.swarm.new
    echo "build.sh: could not install ~/.local/bin/swarm" >&2
    exit 1
  fi
  echo "installed ~/.local/bin/swarm; restart swarm serve to use it"
fi

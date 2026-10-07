#!/bin/sh
# Runs every automated test: Go (with the race detector), vet, formatting, syntax checks, the JavaScript unit tests and
# all the browser suites, each against its own server with a fresh data file. Needs Go, Node, and Playwright with axe
# installed outside the repository (see docs/testing.md):
#   export NODE_PATH=/tmp/gifty-browser/node_modules
#   export GIFTY_CHROME='/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'   # or leave unset for Playwright's
#   tests/run-all.sh            # everything
#   tests/run-all.sh quick      # skips the race detector, which takes a few minutes
set -u
cd "$(dirname "$0")/.."
work=$(mktemp -d)
pids=""
cleanup() { for p in $pids; do kill "$p" 2>/dev/null; done; rm -rf "$work"; }
trap cleanup EXIT INT TERM
failed=""
step() { name=$1; shift; printf '\n== %s\n' "$name"; if "$@"; then :; else failed="$failed\n  $name"; fi; }

go build -o "$work/gifty" . || exit 1
if [ "${1:-}" = quick ]; then step "go test" go test ./...; else step "go test -race" go test -race ./...; fi
step "go vet" go vet ./...
step "gofmt" sh -c 'test -z "$(gofmt -l .)" || { gofmt -l .; exit 1; }'
step "syntax" sh -c 'for f in web/*.js tests/*.cjs; do node --check "$f" || exit 1; done'
step "unit (friends.js date helpers, six time zones)" node tests/unit.cjs

# serve NAME PORT [ENV=VALUE ...] starts a server on a fresh data file and waits for it.
serve() {
  name=$1; port=$2; shift 2
  env GIFTY_ADDR=127.0.0.1:$port GIFTY_DATA="$work/$name.json" "$@" "$work/gifty" >"$work/$name.log" 2>&1 &
  pids="$pids $!"
  i=0; while ! curl -sf "http://127.0.0.1:$port/api/config" >/dev/null; do i=$((i+1)); [ $i -gt 50 ] && { echo "server $name did not start"; cat "$work/$name.log"; exit 1; }; sleep 0.2; done
}
suite() { # suite NAME PORT [ENV=VALUE ...]
  name=$1; port=$2; shift 2
  serve "$name" "$port" "$@"
  step "browser: $name" env GIFTY_TEST_URL=http://127.0.0.1:$port GIFTY_MAIL_LOG="$work/$name.log" node tests/$name.cjs
}
suite browser 8088
suite email 8089 GIFTY_SMTP_HOST=log
suite gate 8090 GIFTY_ACCESS_CODE='test gate code'
suite admin 8091 GIFTY_SMTP_HOST=log GIFTY_ADMINS=boss@example.com
suite features 8092
suite friends 8093
suite friends-more 8094

if [ -n "$failed" ]; then printf '\nFAILED:%b\n' "$failed"; exit 1; fi
printf '\nAll tests passed.\n'

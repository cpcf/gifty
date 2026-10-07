#!/bin/sh
# Runs every automated test: Go (with the race detector), vet, formatting, syntax checks, the JavaScript unit tests and
# all the browser suites, each against its own server with a fresh data file. Needs Go, Node, and Playwright with axe
# installed outside the repository (see docs/testing.md):
#   export NODE_PATH=/tmp/gifty-browser/node_modules
#   export GIFTY_CHROME='/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'   # or leave unset for Playwright's
#   tests/run-all.sh            # everything (what CI runs)
#   tests/run-all.sh quick      # skips the race detector, which takes a few minutes
#   tests/run-all.sh fast       # Go tests, checks and unit tests only: no browser suites
#   tests/run-all.sh auto       # quick if Playwright is installed, otherwise fast, saying so (what the commit hook runs)
set -u
cd "$(dirname "$0")/.."
mode=${1:-full}
# Sensible defaults for the setup in docs/testing.md; set either variable yourself to override.
[ -z "${NODE_PATH:-}" ] && [ -d /tmp/gifty-browser/node_modules ] && export NODE_PATH=/tmp/gifty-browser/node_modules
[ -z "${GIFTY_CHROME:-}" ] && [ -x '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' ] && export GIFTY_CHROME='/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
have_browser() { node -e "require.resolve('playwright');require.resolve('@axe-core/playwright')" 2>/dev/null; }
case "$mode" in
  full|quick|fast) ;;
  auto) if have_browser; then mode=quick; else echo "WARNING: Playwright and axe are not installed (see docs/testing.md), so the browser suites are NOT being run."; mode=fast; fi ;;
  *) echo "usage: tests/run-all.sh [full|quick|fast|auto]"; exit 2 ;;
esac
if [ "$mode" != fast ] && ! have_browser; then echo "Playwright and @axe-core/playwright are needed for the browser suites; see docs/testing.md, or run: tests/run-all.sh fast"; exit 2; fi
work=$(mktemp -d)
pids=""
cleanup() { for p in $pids; do kill "$p" 2>/dev/null; done; rm -rf "$work"; }
trap cleanup EXIT INT TERM
failed=""
# With FAIL_FAST set (the commit hook does) the first failure stops the run.
step() { name=$1; shift; printf '\n== %s\n' "$name"; if "$@"; then :; else failed="$failed\n  $name"; [ -n "${FAIL_FAST:-}" ] && { printf '\nFAILED:%b\n' "$failed"; exit 1; }; fi; }

go build -o "$work/gifty" . || exit 1
if [ "$mode" = full ]; then step "go test -race" go test -race ./...; else step "go test" go test ./...; fi
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
if [ "$mode" != fast ]; then
suite browser 8088
suite email 8089 GIFTY_SMTP_HOST=log
suite gate 8090 GIFTY_ACCESS_CODE='test gate code'
suite admin 8091 GIFTY_SMTP_HOST=log GIFTY_ADMINS=boss@example.com
suite features 8092
suite friends 8093
suite friends-more 8094
fi

if [ -n "$failed" ]; then printf '\nFAILED:%b\n' "$failed"; exit 1; fi
printf '\nAll tests passed (%s).\n' "$mode"

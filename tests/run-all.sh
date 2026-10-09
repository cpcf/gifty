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
# The browser suites listen on seven ports from GIFTY_TEST_PORT_BASE (default 8088, so 8088-8094). Give each copy its
# own base to run several at once, e.g. GIFTY_TEST_PORT_BASE=9300. A port that is already in use stops the run rather
# than letting another server answer for the one under test.
set -u
cd "$(dirname "$0")/.."
mode=${1:-full}
port_base=${GIFTY_TEST_PORT_BASE:-8088}
case "$port_base" in ''|*[!0-9]*) echo "GIFTY_TEST_PORT_BASE must be a port number, not '$port_base'"; exit 2 ;; esac
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

# serve NAME PORT [ENV=VALUE ...] starts a server on a fresh data file and waits for it. It refuses a port that is
# already in use, and checks that the server answering is the one it started, not one left over from another run.
serve() {
  name=$1; port=$2; shift 2
  curl -s -o /dev/null --max-time 2 "http://127.0.0.1:$port/"
  if [ $? -ne 7 ]; then # 7: nothing listening
    echo "port $port is already in use, so suite $name cannot have its own server there."
    echo "Stop whatever is listening (lsof -nP -iTCP:$port -sTCP:LISTEN) or set GIFTY_TEST_PORT_BASE to a free range."
    exit 1
  fi
  env GIFTY_ADDR=127.0.0.1:$port GIFTY_DATA="$work/$name.json" "$@" "$work/gifty" >"$work/$name.log" 2>&1 &
  pid=$!
  pids="$pids $pid"
  i=0
  while ! curl -sf "http://127.0.0.1:$port/api/config" >/dev/null; do
    kill -0 "$pid" 2>/dev/null || { echo "server $name exited before answering"; cat "$work/$name.log"; exit 1; }
    i=$((i+1)); [ $i -gt 50 ] && { echo "server $name did not start"; cat "$work/$name.log"; exit 1; }
    sleep 0.2
  done
  kill -0 "$pid" 2>/dev/null || { echo "server $name exited, and something else answered on port $port"; cat "$work/$name.log"; exit 1; }
}
suite() { # suite NAME OFFSET [ENV=VALUE ...]: OFFSET is added to the port base
  name=$1; port=$((port_base + $2)); shift 2
  serve "$name" "$port" "$@"
  step "browser: $name" env GIFTY_TEST_URL=http://127.0.0.1:$port GIFTY_MAIL_LOG="$work/$name.log" node tests/$name.cjs
}
if [ "$mode" != fast ]; then
suite browser 0
suite email 1 GIFTY_SMTP_HOST=log
suite gate 2 GIFTY_ACCESS_CODE='test gate code'
suite admin 3 GIFTY_SMTP_HOST=log GIFTY_ADMINS=boss@example.com
suite features 4
suite friends 5
suite friends-more 6
fi

if [ -n "$failed" ]; then printf '\nFAILED:%b\n' "$failed"; exit 1; fi
printf '\nAll tests passed (%s).\n' "$mode"

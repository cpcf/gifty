#!/bin/sh
# Check every source module and test without executing browser code.
set -eu
cd "$(dirname "$0")/.."
find web/public tests -type f \( -name '*.js' -o -name '*.cjs' -o -name '*.mjs' \) -print |
  while IFS= read -r file; do
    node --check "$file" || exit 1
  done

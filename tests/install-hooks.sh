#!/bin/sh
# Points this clone at the committed hooks in .githooks (git does not version .git/hooks).
cd "$(dirname "$0")/.." && git config core.hooksPath .githooks && chmod +x .githooks/* tests/run-all.sh && echo "Hooks installed: .githooks/pre-commit now runs the tests when a commit changes code."

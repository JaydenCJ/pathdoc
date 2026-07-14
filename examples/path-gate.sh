#!/usr/bin/env bash
# Uses `pathdoc check` as a local gate — e.g. from a dotfiles Makefile or
# a pre-commit hook — so a broken PATH never silently ships. Exit code 1
# means findings at or above the chosen severity exist.
#
# Usage:  bash examples/path-gate.sh [fail-on]     (fail-on: warn|error)
set -euo pipefail

FAIL_ON="${1:-error}"

if pathdoc check --fail-on "$FAIL_ON"; then
  echo "path-gate: PATH is healthy (threshold: $FAIL_ON)"
else
  echo "path-gate: PATH needs attention — run 'pathdoc report' for details" >&2
  exit 1
fi

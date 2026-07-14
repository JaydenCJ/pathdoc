#!/usr/bin/env bash
# Fabricates a self-contained demo environment for pathdoc: a fake home
# with a .zshrc (pyenv hook + explicit prepend), three bin directories
# with a genuinely shadowed python3, a benign symlinked node, plus a
# duplicate, a dead directory, and an empty PATH segment.
#
# Usage:  bash examples/make-demo-env.sh [target-dir]
# Then run the commands it prints. Offline, idempotent, no host changes.
set -euo pipefail

DEMO="${1:-/tmp/pathdoc-demo}"
rm -rf "$DEMO"
HOME_DIR="$DEMO/home"
SHIMS="$HOME_DIR/.pyenv/shims"
LOCAL="$DEMO/local/bin"
SYS="$DEMO/sys/bin"
DEAD="$DEMO/old/bin" # never created — a dead entry

mkdir -p "$SHIMS" "$LOCAL" "$SYS" "$HOME_DIR"

mkexec() {
  printf '#!/bin/sh\necho "%s"\n' "$1" > "$2"
  chmod 0755 "$2"
}

# python3 exists three times: pyenv shim, local build, system.
mkexec "python 3.12 (pyenv shim)" "$SHIMS/python3"
mkexec "python 3.11 (local build)" "$LOCAL/python3"
mkexec "python 3.10 (system)" "$SYS/python3"

# node exists twice, but the second is a symlink to the first: benign.
mkexec "node v22" "$LOCAL/node"
ln -s "$LOCAL/node" "$SYS/node"

# The rc files that "added" these entries.
cat > "$HOME_DIR/.zshrc" <<RC
eval "\$(pyenv init -)"
export PATH="$LOCAL:\$PATH"
RC

PATH_VALUE="$SHIMS:$LOCAL:$SYS:$LOCAL:$DEAD:"

cat <<MSG
demo environment ready under $DEMO

try:
  pathdoc report --path "$PATH_VALUE" --home "$HOME_DIR" --rc "$HOME_DIR/.zshrc"
  pathdoc which  --path "$PATH_VALUE" --home "$HOME_DIR" --rc "$HOME_DIR/.zshrc" python3
  pathdoc rc     --path "$PATH_VALUE" --home "$HOME_DIR" --rc "$HOME_DIR/.zshrc"
  pathdoc dedupe --path "$PATH_VALUE" --home "$HOME_DIR" --drop-dead --drop-unsafe --emit export
MSG

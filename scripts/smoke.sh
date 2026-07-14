#!/usr/bin/env bash
# End-to-end smoke test for pathdoc: builds the binary, fabricates a
# PATH with shadowed binaries, duplicates, a dead dir, an empty segment,
# and rc files explaining the entries — then asserts on the real CLI
# output of every subcommand. No network, idempotent, finishes in
# seconds.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

fail() {
  echo "SMOKE FAIL: $*" >&2
  exit 1
}

BIN="$WORKDIR/pathdoc"
HOME_DIR="$WORKDIR/home"
SHIMS="$HOME_DIR/.pyenv/shims"
LOCAL="$WORKDIR/local/bin"
SYS="$WORKDIR/sys/bin"
DEAD="$WORKDIR/old/bin" # intentionally never created

mkexec() {
  printf '#!/bin/sh\necho "%s"\n' "$1" > "$2"
  chmod 0755 "$2"
}

echo "1. build"
(cd "$ROOT" && go build -o "$BIN" ./cmd/pathdoc) || fail "go build failed"

echo "2. version matches the manifest"
"$BIN" --version | grep -qx "pathdoc 0.1.0" || fail "--version mismatch"

echo "3. fabricate a tangled PATH with rc provenance"
mkdir -p "$SHIMS" "$LOCAL" "$SYS"
mkexec "pyenv python" "$SHIMS/python3"
mkexec "local python" "$LOCAL/python3"
mkexec "system python" "$SYS/python3"
mkexec "node" "$LOCAL/node"
ln -s "$LOCAL/node" "$SYS/node" # benign: same file through a symlink
cat > "$HOME_DIR/.zshrc" <<RC
eval "\$(pyenv init -)"
export PATH="$LOCAL:\$PATH"
RC
DEMO_PATH="$SHIMS:$LOCAL:$SYS:$LOCAL:$DEAD:"
FLAGS=(--path "$DEMO_PATH" --home "$HOME_DIR" --rc "$HOME_DIR/.zshrc")

echo "4. report diagnoses every entry with provenance"
OUT="$("$BIN" report "${FLAGS[@]}")"
echo "$OUT" | grep -q "6 entries" || fail "entry count missing"
echo "$OUT" | grep -q "duplicate of #2" || fail "duplicate not flagged"
echo "$OUT" | grep -q "does not exist" || fail "dead dir not flagged"
echo "$OUT" | grep -q "empty = current dir" || fail "empty segment not flagged"
echo "$OUT" | grep -q "pyenv hook" || fail "pyenv provenance missing"
echo "$OUT" | grep -q "~/.zshrc:2" || fail "explicit prepend provenance missing"
echo "$OUT" | grep -q "inherited — no rc line found" || fail "inherited marker missing"
echo "$OUT" | grep -q "same file — benign" || fail "benign conflict not labelled"

echo "5. JSON report is machine-readable and correct"
JSON="$("$BIN" report --format json "${FLAGS[@]}")"
echo "$JSON" | grep -q '"tool": "pathdoc"' || fail "json envelope missing"
echo "$JSON" | grep -q '"entries": 6' || fail "json entry count wrong"
echo "$JSON" | grep -q '"benign_conflicts": 1' || fail "json benign count wrong"
echo "$JSON" | grep -q '"duplicate"' || fail "json duplicate issue missing"

echo "6. which shows the winner and every shadowed candidate"
OUT="$("$BIN" which "${FLAGS[@]}" python3)"
echo "$OUT" | grep -q "3 candidate(s) on PATH" || fail "candidate count wrong"
echo "$OUT" | grep -q "► ~/.pyenv/shims/python3" || fail "winner marker missing"
echo "$OUT" | grep -q "shadowed — different file" || fail "shadow verdict missing"
echo "$OUT" | grep -q "added by ~/.zshrc:1 · pyenv hook" || fail "winner provenance missing"

echo "7. which exits 1 for unknown commands"
if "$BIN" which "${FLAGS[@]}" no-such-tool >/dev/null; then
  fail "which should exit 1 when a command is not found"
fi

echo "8. rc lists every PATH-modifying line with evidence"
OUT="$("$BIN" rc "${FLAGS[@]}")"
echo "$OUT" | grep -q "2 PATH-modifying line(s)" || fail "rc mutation count wrong"
echo "$OUT" | grep -q '└─ eval "$(pyenv init -)"' || fail "rc quoted evidence missing"
echo "$OUT" | grep -q "(= entry 2)" || fail "rc matched-entry marker missing"

echo "9. dedupe emits a cleaned PATH"
CLEANED="$("$BIN" dedupe --drop-dead --drop-unsafe "${FLAGS[@]}" 2>/dev/null)"
[ "$CLEANED" = "$SHIMS:$LOCAL:$SYS" ] || fail "dedupe output wrong: $CLEANED"
"$BIN" dedupe --drop-dead --drop-unsafe --emit export "${FLAGS[@]}" 2>/dev/null \
  | grep -q '^export PATH="' || fail "dedupe --emit export malformed"

echo "10. check gates with exit codes"
if "$BIN" check "${FLAGS[@]}" >/dev/null; then
  fail "check should fail on the tangled PATH"
fi
"$BIN" check --no-provenance --path "$LOCAL" >/dev/null \
  || fail "check should pass on a clean single-entry PATH"

echo "11. usage errors exit 2"
set +e
"$BIN" report --format yaml "${FLAGS[@]}" >/dev/null 2>&1
[ $? -eq 2 ] || fail "bad --format should exit 2"
"$BIN" frobnicate >/dev/null 2>&1
[ $? -eq 2 ] || fail "unknown subcommand should exit 2"
set -e

echo "SMOKE OK"

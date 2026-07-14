# Contributing to pathdoc

Issues, discussions and pull requests are all welcome.

## Getting started

You need Go ≥1.22; nothing else. pathdoc is standard library only.

```bash
git clone https://github.com/JaydenCJ/pathdoc && cd pathdoc
go build ./...
go test ./...
bash scripts/smoke.sh
```

`scripts/smoke.sh` builds the binary, fabricates a tangled PATH (shadowed
python3, duplicates, a dead dir, an empty segment, rc files with a pyenv
hook) in a temp dir, and asserts on real CLI output across every
subcommand; it must finish by printing `SMOKE OK`.

## Before you open a pull request

1. `gofmt -l .` reports nothing (formatting is enforced).
2. `go vet ./...` passes with no findings.
3. `go test ./...` passes (91 deterministic tests, no network).
4. `bash scripts/smoke.sh` prints `SMOKE OK`.
5. Add tests for behavior changes; keep logic in pure, unit-testable
   modules (the parsers and matchers never touch the live environment —
   only the CLI layer reads `$PATH`, `$HOME`, and `$SHELL`).

## Ground rules

- Keep dependencies at zero — pathdoc must stay a single static binary
  that builds anywhere Go does.
- No network calls, ever, and no telemetry. pathdoc only reads the
  filesystem, and never writes outside what the user asked for.
- Provenance must never guess: unresolved variables do not match, and
  unparseable lines are recorded as opaque, not interpreted. New rc
  syntax support needs a test reproducing the real rc-file shape.
- New version-manager hooks are data: add a row to the table in
  `internal/rcparse/hooks.go`, a test, and a row in
  `docs/rc-provenance.md`.
- Code comments and doc comments are written in English.
- Determinism first: identical input must produce byte-identical
  reports, including all orderings.

## Reporting bugs

Include the output of `pathdoc version`, the full command you ran, and —
for wrong or missing provenance — the relevant rc line plus the output
of `pathdoc rc` (redact private paths if needed), since that is exactly
what the scanner sees.

## Security

Please do not open public issues for security problems; use GitHub's
private vulnerability reporting on this repository instead.

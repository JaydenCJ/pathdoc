# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-07-12

### Added

- PATH entry classification: textual and symlink-level duplicates, dead
  directories, file-not-directory entries, empty segments (current-dir
  hazard), relative entries, unexpanded `~`, world-writable directories,
  and unreadable directories — each with a severity and an actionable
  explanation.
- Shadowing analysis over the real directory contents: every command
  with multiple providers, winner first, with same-file detection
  (symlink and hardlink aware) so usr-merge and symlink farms are
  labelled benign instead of noise.
- Static rc-file provenance: POSIX `PATH=` assignments (export /
  typeset / declare, trailing `; export PATH`, control-flow-embedded),
  zsh `path=(…)` arrays, fish `set`/`fish_add_path`, macOS
  `/etc/paths(.d)` files, `source`/`.` following with cycle and depth
  guards, and eval hooks for 14 version managers (pyenv, nvm, rustup,
  homebrew, conda, asdf, mise, volta, …). Unresolved variables never
  match; unparseable lines are recorded as opaque, never guessed.
- Subcommands: `report` (full diagnosis with provenance column),
  `which` (every candidate in PATH order with winner, shadow verdicts,
  and the rc line that added each directory), `shadows` (conflict list,
  benign hidden by default), `rc` (every PATH-modifying line with quoted
  evidence and matched entries), `dedupe` (cleaned PATH in plain,
  `export`, or fish form), and `check` (severity-gated exit codes for
  dotfiles hygiene).
- Stable JSON output (`schema_version: 1`) for `report`, `which`,
  `shadows`, and `rc`.
- Full override surface (`--path`, `--home`, `--shell`, `--rc`) so the
  tool can diagnose any PATH — not just the live one — and stay fully
  testable.
- Runnable examples (`examples/make-demo-env.sh`,
  `examples/path-gate.sh`) and a provenance-format reference
  (`docs/rc-provenance.md`).
- 91 deterministic offline tests (unit + in-process CLI integration
  against fabricated environments) and `scripts/smoke.sh`.

[0.1.0]: https://github.com/JaydenCJ/pathdoc/releases/tag/v0.1.0

# pathdoc examples

Two runnable scripts, both offline and self-contained.

## make-demo-env.sh

Fabricates a demo environment with every diagnosis pathdoc makes: a
pyenv-style shim shadowing a local and a system `python3`, a benign
symlinked `node`, a duplicate entry, a dead directory, an empty PATH
segment, and a `.zshrc` whose lines explain where the entries came from.

```bash
bash examples/make-demo-env.sh /tmp/pathdoc-demo
# then run the pathdoc commands the script prints
```

Because the printed commands pass explicit `--path`, `--home`, and
`--rc` values, they never touch your real PATH or rc files, and their
output is identical on every machine (up to the target directory prefix).

## path-gate.sh

Shows `pathdoc check` as a policy gate: it exits non-zero when your
*real* PATH has findings at or above the chosen severity, so it can back
a dotfiles Makefile target or a pre-commit hook.

```bash
bash examples/path-gate.sh error   # fail only on hazards
bash examples/path-gate.sh warn    # also fail on duplicates, dead dirs, shadowing
```

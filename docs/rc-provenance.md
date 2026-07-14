# How rc provenance works

pathdoc's differentiator is answering *which rc line added this PATH
entry*. This document specifies exactly what the scanner understands,
how matches are decided, and where it deliberately stops.

## The model

The scanner reads shell startup files **statically, line by line**. It
never executes anything. Each recognized line becomes a *mutation* with:

| Field | Meaning |
|---|---|
| `file`, `line` | where the logical line starts (continuations join upward) |
| `op` | `replace`, `prepend`, `append`, `mixed`, `hook`, or `opaque` |
| `prepends` / `appends` | the directory segments placed before/after the inherited `$PATH` |
| `tool` | for hooks: the tool recognized (`pyenv`, `nvm`, `homebrew`, …) |
| `via` | the `source`/`.` chain that reached the file, outermost first |

Control flow (`if`, `&&`, loops) is intentionally ignored: for
provenance the question is which line **could** have added an entry, not
which branch ran on a given morning. `pathdoc rc` shows every candidate
line so you can judge.

## Recognized syntax

| Syntax | Example | Notes |
|---|---|---|
| POSIX assignment | `export PATH="$HOME/bin:$PATH"` | also `typeset -x` / `declare -x` prefixes and `PATH=…; export PATH` |
| zsh path array | `path=(~/bin $path)`, `path+=(/x)` | `$path` marks the inherited value |
| fish set | `set -gx PATH $PATH /x` | `fish_user_paths` always counts as a prepend |
| fish helper | `fish_add_path /x` | prepends unless `-a`/`--append` |
| macOS path_helper | one dir per line in `/etc/paths`, `/etc/paths.d/*` | recognized by filename |
| includes | `source file`, `. file`, `[ -f x ] && . x` | followed up to 5 levels, cycles are safe |
| tool hooks | `eval "$(pyenv init -)"`, `\. "$NVM_DIR/nvm.sh"`, `eval "$(brew shellenv)"` | see the hook table below |

Variable expansion covers `~`, `$VAR`, and `${VAR}` against `--home`
plus the process environment. **Unknown variables never match** — the
segment is kept, displayed as `(unresolved)`, and excluded from
attribution, so pathdoc never guesses.

Lines that visibly touch PATH but resist static parsing — command
substitution (`$(…)`, backticks), `${PATH:+…}` tricks, eval-wrapped
assignments — are recorded as `opaque` and listed by `pathdoc rc`, never
interpreted.

## Tool hooks

Version managers rarely write a literal `PATH=` line; they hide the
mutation behind an eval. pathdoc recognizes the invocation and records
the directories the tool is known to add, as expandable patterns
(`*` matches one path component, never `/`):

| Trigger | Tool | Claimed directories |
|---|---|---|
| `pyenv init` | pyenv | `$PYENV_ROOT/shims`, `~/.pyenv/shims` |
| `rbenv init` / `nodenv init` / `goenv init` / `jenv init` | rbenv… | the matching `~/.<tool>/shims` |
| `nvm.sh` | nvm | `$NVM_DIR/versions/node/*/bin`, `~/.nvm/versions/node/*/bin` |
| `cargo/env` | rustup | `$CARGO_HOME/bin`, `~/.cargo/bin` |
| `brew shellenv` | homebrew | `/opt/homebrew/{bin,sbin}`, `/usr/local/{bin,sbin}`, linuxbrew prefixes |
| `sdkman-init.sh` | sdkman | `~/.sdkman/candidates/*/current/bin` |
| `conda.sh`, `conda shell.` | conda | `~/{mini,ana}conda3/{bin,condabin}`, `/opt/conda/…` |
| `asdf.sh` / `mise activate` | asdf / mise | the shim directories |
| `volta setup`, `VOLTA_HOME` | volta | `$VOLTA_HOME/bin`, `~/.volta/bin` |
| `fnm env` | fnm | `~/.local/share/fnm`, `~/.fnm` |

A hook match is labelled as such in output (`~/.zshrc:1 · pyenv hook`) —
it is a strong hint, not a proof.

## Which files are scanned

By default, the startup files of `--shell` (or `$SHELL`) in the order
the shell reads them — e.g. for zsh: `/etc/zshenv`, `~/.zshenv`,
`/etc/zprofile`, `/etc/profile`, `/etc/profile.d/*.sh`, `~/.zprofile`,
`/etc/zshrc`, `~/.zshrc` — plus `/etc/environment`, `/etc/paths`, and
`/etc/paths.d/*`. Missing files are skipped silently. Pass `--rc FILE`
(repeatable) to replace the default set entirely.

## Matching

A live PATH entry matches a mutation segment when the segment's fully
expanded form equals the entry's cleaned path **or** its
symlink-resolved directory, or when a hook glob covers either. All
matching lines are kept in startup order; the renderer shows the first
and counts the rest (`(+1 more)`). Entries no line claims are labelled
`inherited` — they came from the parent process (a login manager, tmux,
an IDE), which is itself a useful diagnosis.

## Known limitations (v0.1.0)

- Only line-level accuracy: a mutation inside a function body is
  attributed to its line, not to the call site.
- Mixed single/double quoting within one value is simplified; a fully
  single-quoted value is treated as literal (matching shell semantics).
- csh/tcsh syntax (`setenv PATH …`) is not yet recognized.
- Windows (`Path`, PowerShell profiles) is out of scope for 0.1.0.

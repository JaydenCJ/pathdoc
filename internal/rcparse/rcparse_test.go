// Tests for the rc-file scanner: every supported PATH-mutation syntax,
// comment/continuation handling, opacity rules, hook recognition,
// source-following, and the /etc/paths format. Files are written to
// t.TempDir(); nothing on the host is read.
package rcparse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scanText writes content as an rc file and scans it with a fixed Env.
func scanText(t *testing.T, content string) []Mutation {
	t.Helper()
	f := filepath.Join(t.TempDir(), "rcfile")
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	muts, _ := Scan([]string{f}, testEnv)
	return muts
}

// one asserts exactly one mutation was found and returns it.
func one(t *testing.T, muts []Mutation) Mutation {
	t.Helper()
	if len(muts) != 1 {
		t.Fatalf("want exactly 1 mutation, got %d: %+v", len(muts), muts)
	}
	return muts[0]
}

func expanded(segs []Segment) []string {
	out := make([]string, 0, len(segs))
	for _, s := range segs {
		out = append(out, s.Expanded)
	}
	return out
}

func TestPosixAssignOps(t *testing.T) {
	cases := []struct {
		line     string
		op       Op
		prepends []string
		appends  []string
	}{
		{`export PATH="$HOME/bin:$PATH"`, OpPrepend, []string{"/home/dev/bin"}, nil},
		{`export PATH="$PATH:/usr/local/go/bin"`, OpAppend, nil, []string{"/usr/local/go/bin"}},
		// No $PATH reference throws the inherited value away — a classic
		// "everything disappeared from my PATH" bug.
		{`PATH=/usr/local/bin:/usr/bin`, OpReplace, []string{"/usr/local/bin", "/usr/bin"}, nil},
		{`export PATH="$HOME/bin:$PATH:$HOME/.local/bin"`, OpMixed, []string{"/home/dev/bin"}, []string{"/home/dev/.local/bin"}},
	}
	for _, c := range cases {
		m := one(t, scanText(t, c.line))
		if m.Op != c.op {
			t.Fatalf("%q: Op = %s, want %s", c.line, m.Op, c.op)
		}
		if got := expanded(m.Prepends); strings.Join(got, ":") != strings.Join(c.prepends, ":") {
			t.Fatalf("%q: prepends = %v, want %v", c.line, got, c.prepends)
		}
		if got := expanded(m.Appends); strings.Join(got, ":") != strings.Join(c.appends, ":") {
			t.Fatalf("%q: appends = %v, want %v", c.line, got, c.appends)
		}
	}
}

func TestAssignmentSpellings(t *testing.T) {
	// The same prepend written the ways real rc files write it: with a
	// trailing export, typeset/declare prefixes, and buried in control
	// flow (line-level scanning ignores branches on purpose).
	for _, line := range []string{
		`PATH=/opt/x/bin:$PATH; export PATH`,
		`typeset -x PATH=/opt/x/bin:$PATH`,
		`declare -x PATH=/opt/x/bin:$PATH`,
		`if [ -d /opt/x/bin ]; then PATH="/opt/x/bin:$PATH"; fi`,
		`[ -d /opt/x/bin ] && PATH=/opt/x/bin:$PATH`,
	} {
		m := one(t, scanText(t, line))
		if m.Op != OpPrepend || expanded(m.Prepends)[0] != "/opt/x/bin" {
			t.Fatalf("%q → %+v", line, m)
		}
	}
}

func TestSingleQuotedValueIsLiteral(t *testing.T) {
	// Single quotes suppress expansion, so '$PATH' is a literal string —
	// this line actually destroys PATH, and must parse as a replace.
	m := one(t, scanText(t, `PATH='$PATH:/opt/x'`))
	if m.Op != OpReplace {
		t.Fatalf("single-quoted $PATH must not count as a reference: %+v", m)
	}
}

func TestUnparseableLinesGoOpaque(t *testing.T) {
	// Wrong guesses would poison provenance; these must all be recorded
	// as opaque rather than interpreted.
	for _, line := range []string{
		`export PATH="$(brew --prefix)/bin:$PATH"`, // command substitution
		"PATH=`mytool prefix`/bin:$PATH",           // backtick substitution
		`PATH=/a${PATH:+:$PATH}`,                   // parameter expansion trick
		`eval "PATH=$PATH:/opt/late/bin"`,          // eval-wrapped assignment
		`path=($(brew --prefix)/bin $path)`,        // substitution in zsh array
	} {
		m := one(t, scanText(t, line))
		if m.Op != OpOpaque {
			t.Fatalf("%q: Op = %s, want opaque", line, m.Op)
		}
	}
}

func TestNonMutationsAreIgnored(t *testing.T) {
	content := strings.Join([]string{
		`PATH=$PATH`,             // self-assignment: a no-op
		`MANPATH=/usr/share/man`, // PATH-like names are not PATH
		`GOPATH=$HOME/go`,
		`CDPATH=.:~`,
		`# export PATH="$HOME/bin:$PATH"`, // commented out
	}, "\n") + "\n"
	if muts := scanText(t, content); len(muts) != 0 {
		t.Fatalf("expected no mutations, got %+v", muts)
	}
}

func TestCommentsLineNumbersAndContinuations(t *testing.T) {
	content := "# leading comment\nalias ll='ls -l'\nexport PATH=\"$HOME/bin:$PATH\"  # my tools\n"
	m := one(t, scanText(t, content))
	if m.Op != OpPrepend || expanded(m.Prepends)[0] != "/home/dev/bin" {
		t.Fatalf("trailing comment broke parsing: %+v", m)
	}
	if m.Line != 3 {
		t.Fatalf("Line = %d, want 3", m.Line)
	}
	// Backslash-newline is deleted like the shell does; the logical line
	// is attributed to where it started.
	m = one(t, scanText(t, "export PATH=\\\n\"$HOME/bin:$PATH\"\n"))
	if m.Op != OpPrepend || expanded(m.Prepends)[0] != "/home/dev/bin" {
		t.Fatalf("continuation not joined: %+v", m)
	}
	if m.Line != 1 {
		t.Fatalf("logical line should be attributed to line 1, got %d", m.Line)
	}
}

func TestLoopVariableStaysUnresolved(t *testing.T) {
	// A PATH prepend driven by a loop variable is still recorded, but
	// its segment must be unresolved so provenance never guesses.
	m := one(t, scanText(t, `for d in /opt/*/bin; do PATH="$d:$PATH"; done`))
	if m.Op != OpPrepend {
		t.Fatalf("got %s, want prepend", m.Op)
	}
	if len(m.Prepends) != 1 || !m.Prepends[0].Unresolved {
		t.Fatalf("loop variable must be unresolved: %+v", m.Prepends)
	}
}

func TestZshPathArray(t *testing.T) {
	m := one(t, scanText(t, `path=(~/bin $path)`))
	if m.Op != OpPrepend || expanded(m.Prepends)[0] != "/home/dev/bin" {
		t.Fatalf("prepend form: %+v", m)
	}
	m = one(t, scanText(t, `path+=(/opt/extra/bin)`))
	if m.Op != OpAppend || expanded(m.Appends)[0] != "/opt/extra/bin" {
		t.Fatalf("append form: %+v", m)
	}
	m = one(t, scanText(t, `path=(/usr/local/bin /usr/bin)`))
	if m.Op != OpReplace || len(m.Prepends) != 2 {
		t.Fatalf("replace form: %+v", m)
	}
}

func TestFishSet(t *testing.T) {
	m := one(t, scanText(t, `set -gx PATH $PATH /usr/local/go/bin`))
	if m.Op != OpAppend || expanded(m.Appends)[0] != "/usr/local/go/bin" {
		t.Fatalf("set PATH append: %+v", m)
	}
	// fish places fish_user_paths ahead of PATH no matter how the set
	// line is written.
	m = one(t, scanText(t, `set -U fish_user_paths /opt/tool/bin $fish_user_paths`))
	if m.Op != OpPrepend || expanded(m.Prepends)[0] != "/opt/tool/bin" {
		t.Fatalf("fish_user_paths: %+v", m)
	}
}

func TestFishAddPath(t *testing.T) {
	m := one(t, scanText(t, `fish_add_path ~/bin /opt/x/bin`))
	if m.Op != OpPrepend || len(m.Prepends) != 2 || expanded(m.Prepends)[0] != "/home/dev/bin" {
		t.Fatalf("default prepend: %+v", m)
	}
	m = one(t, scanText(t, `fish_add_path --append /opt/x/bin`))
	if m.Op != OpAppend || expanded(m.Appends)[0] != "/opt/x/bin" {
		t.Fatalf("--append: %+v", m)
	}
}

func TestHooksAttributeKnownTools(t *testing.T) {
	cases := []struct {
		line, tool, wantSeg string
	}{
		{`eval "$(pyenv init -)"`, "pyenv", "/home/dev/.pyenv/shims"},
		{`eval "$(/opt/homebrew/bin/brew shellenv)"`, "homebrew", "/opt/homebrew/bin"},
		{`. "$HOME/.cargo/env"`, "rustup", "/home/dev/.cargo/bin"},
	}
	for _, c := range cases {
		m := one(t, scanText(t, c.line))
		if m.Op != OpHook || m.Tool != c.tool {
			t.Fatalf("%q → %+v", c.line, m)
		}
		found := false
		for _, s := range m.Prepends {
			if s.Expanded == c.wantSeg {
				found = true
			}
		}
		if !found {
			t.Fatalf("%q: %s not among patterns %v", c.line, c.wantSeg, expanded(m.Prepends))
		}
	}
}

func TestHookNvmProducesGlobPattern(t *testing.T) {
	// nvm's node dirs are versioned; only a glob can cover them.
	m := one(t, scanText(t, `[ -s "$NVM_DIR/nvm.sh" ] && \. "$NVM_DIR/nvm.sh"`))
	if m.Tool != "nvm" {
		t.Fatalf("got %+v", m)
	}
	globs := 0
	for _, s := range m.Prepends {
		if s.Glob {
			globs++
		}
	}
	if globs == 0 {
		t.Fatalf("nvm hook must contribute glob patterns: %+v", m.Prepends)
	}
}

func TestSourceFollowedAndAttributed(t *testing.T) {
	dir := t.TempDir()
	inner := filepath.Join(dir, "env.sh")
	outer := filepath.Join(dir, "bashrc")
	if err := os.WriteFile(inner, []byte(`export PATH="/opt/inner/bin:$PATH"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outer, []byte(`source `+inner+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	muts, read := Scan([]string{outer}, testEnv)
	m := one(t, muts)
	if m.File != inner {
		t.Fatalf("mutation attributed to %q, want the sourced file", m.File)
	}
	if len(m.Via) != 1 || !strings.HasPrefix(m.Via[0], outer+":") {
		t.Fatalf("Via chain = %v, want [%s:1]", m.Via, outer)
	}
	if len(read) != 2 {
		t.Fatalf("filesRead = %v, want both files", read)
	}
}

func TestDotSourceWithGuardFollowed(t *testing.T) {
	dir := t.TempDir()
	inner := filepath.Join(dir, "extra-env")
	outer := filepath.Join(dir, "profile")
	if err := os.WriteFile(inner, []byte(`export PATH="$HOME/extra/bin:$PATH"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	line := `[ -f "` + inner + `" ] && . "` + inner + `"` + "\n"
	if err := os.WriteFile(outer, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	muts, _ := Scan([]string{outer}, testEnv)
	if len(muts) != 1 || muts[0].File != inner {
		t.Fatalf("guarded dot-source not followed: %+v", muts)
	}
}

func TestSourceCycleIsSafe(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("source "+b+"\nexport PATH=/cycle-a:$PATH\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("source "+a+"\nexport PATH=/cycle-b:$PATH\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	muts, _ := Scan([]string{a}, testEnv)
	if len(muts) != 2 {
		t.Fatalf("cycle should still yield both mutations once: %+v", muts)
	}
}

func TestMissingFilesAreSkippedSilently(t *testing.T) {
	muts, read := Scan([]string{filepath.Join(t.TempDir(), "no-such-rc")}, testEnv)
	if len(muts) != 0 || len(read) != 0 {
		t.Fatalf("missing file must contribute nothing: %v %v", muts, read)
	}
}

func TestEtcPathsFormat(t *testing.T) {
	// macOS path_helper files: one directory per line, no shell syntax.
	dir := t.TempDir()
	f := filepath.Join(dir, "paths")
	if err := os.WriteFile(f, []byte("/usr/local/bin\n/usr/bin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	muts, _ := Scan([]string{f}, testEnv)
	if len(muts) != 2 {
		t.Fatalf("want 2 mutations, got %d", len(muts))
	}
	if muts[0].Tool != "path_helper" || muts[0].Op != OpPrepend {
		t.Fatalf("got %+v", muts[0])
	}
	if muts[1].Line != 2 {
		t.Fatalf("line numbers must track the paths file, got %d", muts[1].Line)
	}
}

func TestDefaultRCFileSets(t *testing.T) {
	files := DefaultRCFiles("zsh", "/home/dev")
	idx := func(name string) int {
		for i, f := range files {
			if f == name {
				return i
			}
		}
		t.Fatalf("%s missing from default set %v", name, files)
		return -1
	}
	// zsh reads .zshenv before .zprofile before .zshrc.
	if !(idx("/home/dev/.zshenv") < idx("/home/dev/.zprofile") && idx("/home/dev/.zprofile") < idx("/home/dev/.zshrc")) {
		t.Fatalf("zsh startup order wrong: %v", files)
	}
	contains := func(set []string, name string) bool {
		for _, f := range set {
			if f == name {
				return true
			}
		}
		return false
	}
	bash := DefaultRCFiles("bash", "/home/dev")
	for _, want := range []string{"/etc/profile", "/home/dev/.bash_profile", "/home/dev/.bashrc"} {
		if !contains(bash, want) {
			t.Fatalf("%s missing from bash set %v", want, bash)
		}
	}
	// Unknown shells fall back to the POSIX profile chain.
	if !contains(DefaultRCFiles("tcsh", "/home/dev"), "/home/dev/.profile") {
		t.Fatalf("fallback set must include ~/.profile")
	}
}

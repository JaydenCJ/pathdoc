// In-process integration tests for the CLI: every subcommand, both
// formats, and every exit code, driven through Run() exactly like the
// binary. Fixtures always pass --path/--home/--rc so nothing depends on
// the host's PATH, home directory, or shell configuration.
package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JaydenCJ/pathdoc/internal/version"
)

func writeExec(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// world is a fabricated environment: dirs, rc files, and the PATH that
// ties them together.
type world struct {
	home, shims, local, sys, dead string
	rc                            string
	path                          string
}

// buildWorld fabricates the canonical demo world:
//
//	entry 1  $HOME/.pyenv/shims   python3            added by .zshrc:1 (pyenv hook)
//	entry 2  <root>/local/bin     python3, node      added by .zshrc:2
//	entry 3  <root>/sys/bin       python3, node→2's  inherited
//	entry 4  duplicate of entry 2
//	entry 5  dead directory
//	entry 6  empty segment
func buildWorld(t *testing.T) world {
	t.Helper()
	root := t.TempDir()
	w := world{
		home:  filepath.Join(root, "home"),
		local: filepath.Join(root, "local", "bin"),
		sys:   filepath.Join(root, "sys", "bin"),
		dead:  filepath.Join(root, "gone", "bin"),
	}
	w.shims = filepath.Join(w.home, ".pyenv", "shims")
	writeExec(t, filepath.Join(w.shims, "python3"))
	writeExec(t, filepath.Join(w.local, "python3"))
	node := writeExec(t, filepath.Join(w.local, "node"))
	writeExec(t, filepath.Join(w.sys, "python3"))
	if err := os.Symlink(node, filepath.Join(w.sys, "node")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	w.rc = filepath.Join(w.home, ".zshrc")
	content := "eval \"$(pyenv init -)\"\nexport PATH=\"" + w.local + ":$PATH\"\n"
	if err := os.WriteFile(w.rc, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	w.path = strings.Join([]string{w.shims, w.local, w.sys, w.local, w.dead, ""}, ":")
	return w
}

func (w world) flags() []string {
	return []string{"--path", w.path, "--home", w.home, "--rc", w.rc}
}

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code := Run(args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestVersionSubcommandAndAlias(t *testing.T) {
	code, out, _ := run(t, "version")
	if code != ExitOK || out != "pathdoc "+version.Version+"\n" {
		t.Fatalf("code=%d out=%q", code, out)
	}
	code, out, _ = run(t, "--version")
	if code != ExitOK || !strings.Contains(out, version.Version) {
		t.Fatalf("alias: code=%d out=%q", code, out)
	}
}

func TestHelpExitsZero(t *testing.T) {
	code, out, _ := run(t, "help")
	if code != ExitOK || !strings.Contains(out, "usage:") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestSubcommandHelpExitsZero(t *testing.T) {
	// An explicit help request must not exit like a usage error: dotfiles
	// scripts that `set -e` would otherwise die on `pathdoc report --help`.
	code, _, errOut := run(t, "report", "--help")
	if code != ExitOK {
		t.Fatalf("code=%d, want %d", code, ExitOK)
	}
	if !strings.Contains(errOut, "-format") || !strings.Contains(errOut, "-no-provenance") {
		t.Fatalf("flag listing missing from help output: %q", errOut)
	}
}

func TestUsageErrorsExitTwo(t *testing.T) {
	w := buildWorld(t)
	cases := []struct {
		name string
		args []string
		want string // required stderr fragment ("" = any)
	}{
		{"unknown subcommand", []string{"frobnicate"}, "unknown subcommand"},
		{"unknown flag", []string{"report", "--definitely-not-a-flag"}, ""},
		{"bad format", append([]string{"report", "--format", "yaml"}, w.flags()...), "unsupported --format"},
		{"bad emit", append([]string{"dedupe", "--emit", "csh"}, w.flags()...), "unsupported --emit"},
		{"bad fail-on", []string{"check", "--fail-on", "fatal", "--no-provenance", "--path", "/nonexistent-gate"}, "unsupported --fail-on"},
	}
	for _, c := range cases {
		code, _, errOut := run(t, c.args...)
		if code != ExitUsage {
			t.Fatalf("%s: code=%d, want %d", c.name, code, ExitUsage)
		}
		if c.want != "" && !strings.Contains(errOut, c.want) {
			t.Fatalf("%s: stderr %q missing %q", c.name, errOut, c.want)
		}
	}
}

func TestReportText(t *testing.T) {
	w := buildWorld(t)
	code, out, _ := run(t, append([]string{"report"}, w.flags()...)...)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	for _, want := range []string{
		"6 entries",
		"~/.pyenv/shims",
		"duplicate of #2",
		"does not exist",
		"empty = current dir",
		"pyenv hook",
		"(inherited — no rc line found)",
		"python3",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("report missing %q:\n%s", want, out)
		}
	}
}

func TestBareFlagsMeanReport(t *testing.T) {
	w := buildWorld(t)
	code, out, _ := run(t, w.flags()...)
	if code != ExitOK || !strings.Contains(out, "pathdoc report") {
		t.Fatalf("bare flags should run report: code=%d\n%s", code, out)
	}
}

func TestReportJSONIsValid(t *testing.T) {
	w := buildWorld(t)
	code, out, _ := run(t, append([]string{"report", "--format", "json"}, w.flags()...)...)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	var got struct {
		Tool    string `json:"tool"`
		Summary struct {
			Entries   int `json:"entries"`
			Conflicts int `json:"conflicts"`
		} `json:"summary"`
		RCFiles []string `json:"rc_files"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if got.Tool != "pathdoc" || got.Summary.Entries != 6 || got.Summary.Conflicts != 2 {
		t.Fatalf("summary wrong: %+v", got)
	}
	if len(got.RCFiles) != 1 {
		t.Fatalf("rc_files = %v", got.RCFiles)
	}
}

func TestNoProvenanceOmitsColumn(t *testing.T) {
	w := buildWorld(t)
	code, out, _ := run(t, "report", "--path", w.path, "--home", w.home, "--no-provenance")
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if strings.Contains(out, "provenance:") || strings.Contains(out, "(inherited") {
		t.Fatalf("--no-provenance output still shows rc data:\n%s", out)
	}
}

func TestWhichWinnerShadowedAndProvenance(t *testing.T) {
	w := buildWorld(t)
	// The flag package wants flags before positional names.
	code, out, _ := run(t, append(append([]string{"which"}, w.flags()...), "python3")...)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out, "3 candidate(s)") {
		t.Fatalf("candidate count missing:\n%s", out)
	}
	if !strings.Contains(out, "► ~/.pyenv/shims/python3") {
		t.Fatalf("winner marker missing:\n%s", out)
	}
	if !strings.Contains(out, "added by ~/.zshrc:1 · pyenv hook") {
		t.Fatalf("provenance missing:\n%s", out)
	}
	if !strings.Contains(out, "shadowed — different file") {
		t.Fatalf("shadow verdict missing:\n%s", out)
	}
}

func TestWhichNotFoundExitsOne(t *testing.T) {
	w := buildWorld(t)
	code, out, _ := run(t, append(append([]string{"which"}, w.flags()...), "ghost-command")...)
	if code != ExitFindings {
		t.Fatalf("code=%d, want %d", code, ExitFindings)
	}
	if !strings.Contains(out, "not found on PATH") {
		t.Fatalf("message missing:\n%s", out)
	}
	// One hit plus one miss must still signal the miss.
	code, _, _ = run(t, append(append([]string{"which"}, w.flags()...), "python3", "ghost-command")...)
	if code != ExitFindings {
		t.Fatalf("mixed: code=%d, want %d", code, ExitFindings)
	}
}

func TestWhichWithoutArgsIsUsageError(t *testing.T) {
	w := buildWorld(t)
	code, _, errOut := run(t, append([]string{"which"}, w.flags()...)...)
	if code != ExitUsage || !strings.Contains(errOut, "command name") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestWhichJSON(t *testing.T) {
	w := buildWorld(t)
	code, out, _ := run(t, append(append([]string{"which", "--format", "json"}, w.flags()...), "node")...)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	var got struct {
		Results []struct {
			Name       string `json:"name"`
			Found      bool   `json:"found"`
			Candidates []struct {
				Wins             bool `json:"wins"`
				SameFileAsWinner bool `json:"same_file_as_winner"`
			} `json:"candidates"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	r := got.Results[0]
	if !r.Found || len(r.Candidates) != 2 {
		t.Fatalf("node result wrong: %+v", r)
	}
	if !r.Candidates[1].SameFileAsWinner {
		t.Fatal("symlinked node must be same_file_as_winner")
	}
}

func TestShadowsHidesBenignByDefault(t *testing.T) {
	w := buildWorld(t)
	code, out, _ := run(t, append([]string{"shadows"}, w.flags()...)...)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out, "python3 — 3 candidates") {
		t.Fatalf("python3 conflict missing:\n%s", out)
	}
	if strings.Contains(out, "node — 2 candidates") {
		t.Fatalf("benign node conflict should be hidden:\n%s", out)
	}
}

func TestShadowsAllIncludesBenign(t *testing.T) {
	w := buildWorld(t)
	_, out, _ := run(t, append([]string{"shadows", "--all"}, w.flags()...)...)
	if !strings.Contains(out, "node — 2 candidates") {
		t.Fatalf("--all should include the benign conflict:\n%s", out)
	}
}

func TestRCListsEveryMutation(t *testing.T) {
	w := buildWorld(t)
	code, out, _ := run(t, append([]string{"rc"}, w.flags()...)...)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out, "2 PATH-modifying line(s) from 1 rc file(s)") {
		t.Fatalf("header wrong:\n%s", out)
	}
	if !strings.Contains(out, "[hook] pyenv") || !strings.Contains(out, "[prepend]") {
		t.Fatalf("ops missing:\n%s", out)
	}
	if !strings.Contains(out, "(= entry 2)") {
		t.Fatalf("matched-entry marker missing:\n%s", out)
	}
}

func TestDedupeRemovesDuplicatesOnly(t *testing.T) {
	w := buildWorld(t)
	code, out, errOut := run(t, append([]string{"dedupe"}, w.flags()...)...)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	want := strings.Join([]string{w.shims, w.local, w.sys, w.dead, ""}, ":") + "\n"
	if out != want {
		t.Fatalf("dedupe output:\n got %q\nwant %q", out, want)
	}
	if !strings.Contains(errOut, "kept 5 of 6") {
		t.Fatalf("summary missing: %q", errOut)
	}
}

func TestDedupeDropDeadAndUnsafe(t *testing.T) {
	w := buildWorld(t)
	code, out, _ := run(t, append([]string{"dedupe", "--drop-dead", "--drop-unsafe"}, w.flags()...)...)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	want := strings.Join([]string{w.shims, w.local, w.sys}, ":") + "\n"
	if out != want {
		t.Fatalf("dedupe output:\n got %q\nwant %q", out, want)
	}
}

func TestDedupeEmitForms(t *testing.T) {
	w := buildWorld(t)
	_, out, _ := run(t, append([]string{"dedupe", "--drop-dead", "--drop-unsafe", "--emit", "export"}, w.flags()...)...)
	want := "export PATH=\"" + strings.Join([]string{w.shims, w.local, w.sys}, ":") + "\"\n"
	if out != want {
		t.Fatalf("export: got %q\nwant %q", out, want)
	}
	_, out, _ = run(t, append([]string{"dedupe", "--drop-dead", "--drop-unsafe", "--emit", "fish"}, w.flags()...)...)
	if !strings.HasPrefix(out, "set -gx PATH ") || !strings.Contains(out, w.shims) {
		t.Fatalf("fish: got %q", out)
	}
}

func TestCheckFailsOnWorldFindings(t *testing.T) {
	w := buildWorld(t)
	code, out, _ := run(t, append([]string{"check"}, w.flags()...)...)
	if code != ExitFindings {
		t.Fatalf("code=%d, want %d", code, ExitFindings)
	}
	if !strings.Contains(out, "check: FAIL") {
		t.Fatalf("verdict missing:\n%s", out)
	}
	if !strings.Contains(out, "[error]") || !strings.Contains(out, "[warn ]") {
		t.Fatalf("findings missing:\n%s", out)
	}
}

func TestCheckFailOnErrorIgnoresWarnings(t *testing.T) {
	// Keep only warn-level findings: a duplicate entry.
	dir := filepath.Join(t.TempDir(), "bin")
	writeExec(t, filepath.Join(dir, "tool"))
	code, out, _ := run(t, "check", "--fail-on", "error", "--no-provenance", "--path", dir+":"+dir)
	if code != ExitOK {
		t.Fatalf("code=%d, want 0 (warnings only)\n%s", code, out)
	}
	if !strings.Contains(out, "check: ok") {
		t.Fatalf("verdict missing:\n%s", out)
	}
}

func TestCheckPassesOnCleanPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bin")
	writeExec(t, filepath.Join(dir, "tool"))
	code, out, _ := run(t, "check", "--no-provenance", "--path", dir)
	if code != ExitOK || !strings.Contains(out, "check: ok") {
		t.Fatalf("code=%d\n%s", code, out)
	}
}

func TestTildeRCFlagExpandsAgainstHome(t *testing.T) {
	w := buildWorld(t)
	code, out, _ := run(t, "report", "--path", w.path, "--home", w.home, "--rc", "~/.zshrc")
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out, "pyenv hook") {
		t.Fatalf("~/.zshrc was not resolved against --home:\n%s", out)
	}
}

func TestExplicitlyEmptyPathIsNotTheLivePath(t *testing.T) {
	// `--path ""` must diagnose an explicitly empty PATH, not silently fall
	// back to $PATH — that fallback would hide exactly the hazard the caller
	// is probing.
	code, out, _ := run(t, "report", "--no-provenance", "--path", "")
	if code != ExitOK {
		t.Fatalf("code=%d\n%s", code, out)
	}
	if !strings.Contains(out, "0 entries") || !strings.Contains(out, "PATH is empty") {
		t.Fatalf("empty --path fell back to the live PATH:\n%s", out)
	}
}

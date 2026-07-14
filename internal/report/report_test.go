// Tests for the text and JSON renderers. A shared fixture fabricates a
// PATH with a pyenv-style shim dir, a duplicate, a dead dir, an empty
// segment, and both benign and distinct shadowing — the shapes the
// renderers must make legible.
package report

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JaydenCJ/pathdoc/internal/pathenv"
	"github.com/JaydenCJ/pathdoc/internal/provenance"
	"github.com/JaydenCJ/pathdoc/internal/rcparse"
	"github.com/JaydenCJ/pathdoc/internal/scan"
	"github.com/JaydenCJ/pathdoc/internal/shadow"
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

// fixture builds the shared model. Layout:
//
//	entry 1  ~/.pyenv/shims        python3            (rc: pyenv hook)
//	entry 2  <root>/local/bin      python3, node      (rc: explicit prepend)
//	entry 3  <root>/sys/bin        python3, node→2's  (inherited)
//	entry 4  duplicate of entry 2
//	entry 5  dead directory
//	entry 6  empty segment
func fixture(t *testing.T) *Model {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	shims := filepath.Join(home, ".pyenv", "shims")
	local := filepath.Join(root, "local", "bin")
	sys := filepath.Join(root, "sys", "bin")
	writeExec(t, filepath.Join(shims, "python3"))
	writeExec(t, filepath.Join(local, "python3"))
	node := writeExec(t, filepath.Join(local, "node"))
	writeExec(t, filepath.Join(sys, "python3"))
	if err := os.Symlink(node, filepath.Join(sys, "node")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	rc := filepath.Join(home, ".zshrc")
	content := "eval \"$(pyenv init -)\"\nexport PATH=\"" + local + ":$PATH\"\n"
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rc, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	pathStr := strings.Join([]string{shims, local, sys, local, filepath.Join(root, "gone"), ""}, ":")
	entries := pathenv.Classify(pathStr)
	idx := scan.Build(entries)
	muts, read := rcparse.Scan([]string{rc}, rcparse.Env{Home: home})
	return &Model{
		Path:      pathStr,
		Entries:   entries,
		Index:     idx,
		Conflicts: shadow.Analyze(idx),
		Prov:      provenance.Attribute(entries, muts, read),
		Home:      home,
	}
}

func render(t *testing.T, m *Model) string {
	t.Helper()
	var buf bytes.Buffer
	Text(&buf, m)
	return buf.String()
}

func TestTextHeaderCountsAndAbbreviation(t *testing.T) {
	out := render(t, fixture(t))
	if !strings.Contains(out, "6 entries") {
		t.Fatalf("header entry count missing:\n%s", out)
	}
	if !strings.Contains(out, "2 shadowing conflict(s) (1 benign)") {
		t.Fatalf("conflict summary wrong:\n%s", out)
	}
	if !strings.Contains(out, "~/.pyenv/shims") {
		t.Fatalf("home not abbreviated:\n%s", out)
	}
}

func TestTextProvenanceColumn(t *testing.T) {
	out := render(t, fixture(t))
	if !strings.Contains(out, "~/.zshrc:1 · pyenv hook") {
		t.Fatalf("pyenv hook provenance missing:\n%s", out)
	}
	if !strings.Contains(out, "~/.zshrc:2") {
		t.Fatalf("explicit prepend provenance missing:\n%s", out)
	}
	if !strings.Contains(out, "(inherited — no rc line found)") {
		t.Fatalf("inherited marker missing:\n%s", out)
	}
}

func TestTextListsIssuesWithSeverity(t *testing.T) {
	out := render(t, fixture(t))
	for _, want := range []string{"[warn ]", "[error]", "duplicates entry 2", "does not exist", "current directory"} {
		if !strings.Contains(out, want) {
			t.Fatalf("issue line %q missing:\n%s", want, out)
		}
	}
}

func TestTextShadowSection(t *testing.T) {
	out := render(t, fixture(t))
	if !strings.Contains(out, "python3") || !strings.Contains(out, "wins") {
		t.Fatalf("shadow section missing python3 conflict:\n%s", out)
	}
	if !strings.Contains(out, "same file — benign") {
		t.Fatalf("benign node conflict not labelled:\n%s", out)
	}
}

func TestTextCleanPathSaysSo(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bin")
	writeExec(t, filepath.Join(dir, "solo"))
	entries := pathenv.Classify(dir)
	idx := scan.Build(entries)
	m := &Model{Path: dir, Entries: entries, Index: idx, Conflicts: shadow.Analyze(idx)}
	out := render(t, m)
	if !strings.Contains(out, "no issues found.") || !strings.Contains(out, "no shadowing conflicts.") {
		t.Fatalf("clean-path wording missing:\n%s", out)
	}
}

func TestWhichTextWinnerAndProvenance(t *testing.T) {
	m := fixture(t)
	var buf bytes.Buffer
	missing := WhichText(&buf, m, []string{"python3"})
	out := buf.String()
	if missing != 0 {
		t.Fatalf("python3 should be found, missing=%d", missing)
	}
	if !strings.Contains(out, "3 candidate(s)") {
		t.Fatalf("candidate count wrong:\n%s", out)
	}
	if !strings.Contains(out, "► ~/.pyenv/shims/python3") {
		t.Fatalf("winner marker missing:\n%s", out)
	}
	if !strings.Contains(out, "shadowed — different file") {
		t.Fatalf("shadow verdict missing:\n%s", out)
	}
	if !strings.Contains(out, "added by ~/.zshrc:1 · pyenv hook") {
		t.Fatalf("winner provenance missing:\n%s", out)
	}
	// A name with no providers is reported and counted.
	buf.Reset()
	if missing := WhichText(&buf, m, []string{"no-such-tool"}); missing != 1 {
		t.Fatalf("missing = %d, want 1", missing)
	}
	if !strings.Contains(buf.String(), "not found on PATH") {
		t.Fatalf("not-found message missing:\n%s", buf.String())
	}
}

func TestShadowsTextBenignFilter(t *testing.T) {
	m := fixture(t)
	var buf bytes.Buffer
	ShadowsText(&buf, m, false)
	out := buf.String()
	if strings.Contains(out, "node —") {
		t.Fatalf("benign node conflict should be hidden:\n%s", out)
	}
	if !strings.Contains(out, "1 benign hidden") {
		t.Fatalf("hidden count missing:\n%s", out)
	}
	buf.Reset()
	ShadowsText(&buf, m, true)
	if !strings.Contains(buf.String(), "node — 2 candidates") {
		t.Fatalf("--all should show the benign conflict:\n%s", buf.String())
	}
}

func TestRCTextListsMutationsWithEvidence(t *testing.T) {
	m := fixture(t)
	var buf bytes.Buffer
	RCText(&buf, m)
	out := buf.String()
	if !strings.Contains(out, "~/.zshrc:1  [hook] pyenv") {
		t.Fatalf("hook line missing:\n%s", out)
	}
	if !strings.Contains(out, "└─ eval \"$(pyenv init -)\"") {
		t.Fatalf("quoted evidence missing:\n%s", out)
	}
	if !strings.Contains(out, "(= entry 1)") {
		t.Fatalf("matched-entry marker missing:\n%s", out)
	}
}

func TestFindingsSeveritiesAndConflicts(t *testing.T) {
	m := fixture(t)
	fs := Findings(m)
	var errors, warns, infos int
	for _, f := range fs {
		switch f.Severity {
		case pathenv.Error:
			errors++
		case pathenv.Warn:
			warns++
		default:
			infos++
		}
	}
	// empty segment → error; duplicate + dead + python3 conflict → warns;
	// benign node conflict → info.
	if errors != 1 || warns != 3 || infos != 1 {
		t.Fatalf("severity split = %d/%d/%d (err/warn/info), findings: %+v", errors, warns, infos, fs)
	}
}

func TestJSONEnvelopeSummaryAndEntries(t *testing.T) {
	m := fixture(t)
	var buf bytes.Buffer
	if err := JSON(&buf, m); err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if env["tool"] != "pathdoc" || env["schema_version"] != float64(1) {
		t.Fatalf("envelope wrong: %v", env)
	}
	sum := env["summary"].(map[string]any)
	if sum["entries"] != float64(6) || sum["conflicts"] != float64(2) || sum["benign_conflicts"] != float64(1) {
		t.Fatalf("summary wrong: %v", sum)
	}
	var got struct {
		Entries []struct {
			Index       int      `json:"index"`
			Issues      []string `json:"issues"`
			DuplicateOf int      `json:"duplicate_of"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	// Indexes are 1-based to match the text output exactly.
	if got.Entries[0].Index != 1 {
		t.Fatalf("first entry index = %d, want 1", got.Entries[0].Index)
	}
	if got.Entries[0].Issues == nil {
		t.Fatal("issues must be [] even when clean, not null")
	}
	if dup := got.Entries[3]; dup.DuplicateOf != 2 {
		t.Fatalf("duplicate_of = %d, want 2 (1-based)", dup.DuplicateOf)
	}
}

func TestJSONShadowProviders(t *testing.T) {
	m := fixture(t)
	var buf bytes.Buffer
	if err := JSON(&buf, m); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Shadows []struct {
			Name      string `json:"name"`
			Benign    bool   `json:"benign"`
			Providers []struct {
				Wins             bool `json:"wins"`
				SameFileAsWinner bool `json:"same_file_as_winner"`
			} `json:"providers"`
		} `json:"shadows"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Shadows) != 2 {
		t.Fatalf("want 2 shadows, got %d", len(got.Shadows))
	}
	// sorted: node before python3
	if got.Shadows[0].Name != "node" || !got.Shadows[0].Benign {
		t.Fatalf("node conflict wrong: %+v", got.Shadows[0])
	}
	py := got.Shadows[1]
	if !py.Providers[0].Wins || py.Providers[1].SameFileAsWinner {
		t.Fatalf("python3 providers wrong: %+v", py.Providers)
	}
}

func TestWhichJSONNotFound(t *testing.T) {
	m := fixture(t)
	var buf bytes.Buffer
	if err := WhichJSON(&buf, m, []string{"ghost"}); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Results []struct {
			Found      bool  `json:"found"`
			Candidates []any `json:"candidates"`
		} `json:"results"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Results[0].Found || len(got.Results[0].Candidates) != 0 {
		t.Fatalf("ghost should be not-found with empty candidates: %+v", got.Results[0])
	}
}

func TestAbbrev(t *testing.T) {
	if got := Abbrev("/home/dev/bin", "/home/dev"); got != "~/bin" {
		t.Fatalf("got %q", got)
	}
	if got := Abbrev("/home/devious/bin", "/home/dev"); got != "/home/devious/bin" {
		t.Fatalf("prefix match must respect the separator, got %q", got)
	}
	if got := Abbrev("/home/dev", "/home/dev"); got != "~" {
		t.Fatalf("got %q", got)
	}
}

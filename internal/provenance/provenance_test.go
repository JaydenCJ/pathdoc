// Tests for provenance matching: rc segments against live PATH entries.
// Entries come from fabricated directories; mutations are built by
// scanning fabricated rc files, so this exercises the real pipeline.
package provenance

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JaydenCJ/pathdoc/internal/pathenv"
	"github.com/JaydenCJ/pathdoc/internal/rcparse"
)

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func scanRC(t *testing.T, home, content string) ([]rcparse.Mutation, []string) {
	t.Helper()
	f := filepath.Join(t.TempDir(), "rc")
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return rcparse.Scan([]string{f}, rcparse.Env{Home: home})
}

func TestExactMatch(t *testing.T) {
	dir := mkdir(t, t.TempDir(), "tool", "bin")
	entries := pathenv.Classify(dir)
	muts, read := scanRC(t, "", "export PATH=\""+dir+":$PATH\"\n")
	res := Attribute(entries, muts, read)
	refs := res.Refs[0]
	if len(refs) != 1 {
		t.Fatalf("want 1 ref, got %+v", refs)
	}
	if refs[0].Line != 1 || refs[0].Op != rcparse.OpPrepend {
		t.Fatalf("ref = %+v", refs[0])
	}
}

func TestMatchViaHomeExpansion(t *testing.T) {
	home := t.TempDir()
	dir := mkdir(t, home, ".local", "bin")
	entries := pathenv.Classify(dir)
	muts, read := scanRC(t, home, `export PATH="$HOME/.local/bin:$PATH"`+"\n")
	res := Attribute(entries, muts, read)
	if len(res.Refs[0]) != 1 {
		t.Fatalf("$HOME segment should match the concrete dir: %+v", res.Refs)
	}
}

func TestGlobMatchForVersionedToolDirs(t *testing.T) {
	// nvm's node dirs are versioned; only the hook's glob pattern can
	// claim them.
	home := t.TempDir()
	dir := mkdir(t, home, ".nvm", "versions", "node", "v20.1.0", "bin")
	entries := pathenv.Classify(dir)
	muts, read := scanRC(t, home, `[ -s "$NVM_DIR/nvm.sh" ] && \. "$NVM_DIR/nvm.sh"`+"\n")
	res := Attribute(entries, muts, read)
	refs := res.Refs[0]
	if len(refs) != 1 || refs[0].Tool != "nvm" {
		t.Fatalf("glob hook should claim the versioned dir: %+v", refs)
	}
}

func TestNoMatchLeavesEntryUnexplained(t *testing.T) {
	dir := mkdir(t, t.TempDir(), "inherited", "bin")
	entries := pathenv.Classify(dir)
	muts, read := scanRC(t, "", `export PATH="/somewhere/else:$PATH"`+"\n")
	res := Attribute(entries, muts, read)
	if len(res.Refs[0]) != 0 {
		t.Fatalf("unrelated rc line must not match: %+v", res.Refs[0])
	}
}

func TestMultipleRefsKeepStartupOrder(t *testing.T) {
	dir := mkdir(t, t.TempDir(), "bin")
	entries := pathenv.Classify(dir)
	content := "export PATH=\"" + dir + ":$PATH\"\nexport PATH=\"$PATH:" + dir + "\"\n"
	muts, read := scanRC(t, "", content)
	res := Attribute(entries, muts, read)
	refs := res.Refs[0]
	if len(refs) != 2 {
		t.Fatalf("both lines mention the dir: %+v", refs)
	}
	if refs[0].Line != 1 || refs[1].Line != 2 {
		t.Fatalf("refs out of startup order: %+v", refs)
	}
}

func TestUnresolvedSegmentNeverMatches(t *testing.T) {
	dir := mkdir(t, t.TempDir(), "bin")
	entries := pathenv.Classify(dir)
	// $UNSET_TOOL_HOME cannot be expanded — even though the literal tail
	// looks similar, no guess may be recorded.
	muts, read := scanRC(t, "", `export PATH="$UNSET_TOOL_HOME/bin:$PATH"`+"\n")
	res := Attribute(entries, muts, read)
	if len(res.Refs[0]) != 0 {
		t.Fatalf("unresolved segment matched: %+v", res.Refs[0])
	}
}

func TestSymlinkedEntryMatchesResolvedTarget(t *testing.T) {
	root := t.TempDir()
	real := mkdir(t, root, "versions", "current", "bin")
	link := filepath.Join(root, "bin")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	entries := pathenv.Classify(link)
	// The rc file names the resolved target, PATH holds the symlink.
	muts, read := scanRC(t, "", "export PATH=\""+real+":$PATH\"\n")
	res := Attribute(entries, muts, read)
	if len(res.Refs[0]) != 1 {
		t.Fatalf("resolved-target match failed: %+v", res.Refs)
	}
}

func TestDuplicateEntriesShareTheSameRef(t *testing.T) {
	dir := mkdir(t, t.TempDir(), "bin")
	entries := pathenv.Classify(dir + ":" + dir)
	muts, read := scanRC(t, "", "export PATH=\""+dir+":$PATH\"\n")
	res := Attribute(entries, muts, read)
	if len(res.Refs[0]) != 1 || len(res.Refs[1]) != 1 {
		t.Fatalf("both occurrences should be attributed: %+v", res.Refs)
	}
}

func TestMatchingPrimitives(t *testing.T) {
	// Empty and relative entries have no candidate strings at all, so
	// they can never be (mis)attributed.
	entries := pathenv.Classify(":relative/bin")
	if got := Candidates(entries[0]); len(got) != 0 {
		t.Fatalf("empty entry has no candidates, got %v", got)
	}
	if got := Candidates(entries[1]); len(got) != 0 {
		t.Fatalf("relative entry has no candidates, got %v", got)
	}
	// Glob matching is component-wise: * never crosses a separator.
	seg := rcparse.Segment{Expanded: "/x/*/bin", Glob: true}
	if MatchSegment(seg, []string{"/x/a/b/bin"}) {
		t.Fatal("* must not match across path separators")
	}
	if !MatchSegment(seg, []string{"/x/a/bin"}) {
		t.Fatal("single-component glob should match")
	}
}

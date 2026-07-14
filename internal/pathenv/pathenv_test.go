// Tests for PATH splitting and entry classification. Every case builds
// its own directory tree under t.TempDir(), so nothing depends on the
// host's PATH, home, or installed tools.
package pathenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSplit(t *testing.T) {
	if got := Split(""); len(got) != 0 {
		t.Fatalf("Split(\"\") = %q, want empty", got)
	}
	// ":" boundaries produce empty segments, and each one means "search
	// the current directory" — they must never be silently dropped.
	got := Split("/a::/b:")
	want := []string{"/a", "", "/b", ""}
	if len(got) != len(want) {
		t.Fatalf("got %d segments %q, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("segment %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestClassifyHealthyDirectoryIsClean(t *testing.T) {
	dir := mkdir(t, t.TempDir(), "bin")
	es := Classify(dir)
	if len(es) != 1 {
		t.Fatalf("want 1 entry, got %d", len(es))
	}
	e := es[0]
	if !e.OK() || !e.Exists || !e.IsDir {
		t.Fatalf("healthy dir misclassified: %+v", e)
	}
	if e.Resolved == "" {
		t.Fatal("expected Resolved to be filled for an existing dir")
	}
	if e.MaxSeverity() != Info {
		t.Fatalf("clean entry MaxSeverity = %s, want info", e.MaxSeverity())
	}
}

func TestClassifyDeadAndNotDirEntries(t *testing.T) {
	dead := filepath.Join(t.TempDir(), "definitely", "missing")
	if e := Classify(dead)[0]; !e.Has(IssueDead) || e.Exists {
		t.Fatalf("missing dir should be dead: %+v", e)
	}
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if e := Classify(f)[0]; !e.Has(IssueNotDir) || !e.Exists || e.IsDir {
		t.Fatalf("file entry should be not-dir: %+v", e)
	}
}

func TestClassifyHazardSegments(t *testing.T) {
	// Empty segments mean $PWD, relative segments depend on $PWD, and a
	// literal ~ survives into PATH when the assignment was quoted —
	// execvp will not expand it. All three change what runs.
	dir := mkdir(t, t.TempDir(), "bin")
	es := Classify(dir + "::" + dir)
	if !es[1].Has(IssueEmpty) {
		t.Fatalf("empty segment not flagged: %+v", es[1])
	}
	if es[1].Clean != "" {
		t.Fatalf("empty segment must keep Clean empty, got %q", es[1].Clean)
	}
	if es[1].MaxSeverity() != Error {
		t.Fatalf("empty entry MaxSeverity = %s, want error", es[1].MaxSeverity())
	}
	if e := Classify("bin/tools")[0]; !e.Has(IssueRelative) {
		t.Fatalf("relative entry not flagged: %+v", e)
	}
	for _, raw := range []string{"~", "~/bin"} {
		if e := Classify(raw)[0]; !e.Has(IssueTilde) {
			t.Fatalf("%q not flagged as tilde: %+v", raw, e)
		}
	}
}

func TestClassifyTextualDuplicates(t *testing.T) {
	dir := mkdir(t, t.TempDir(), "bin")
	es := Classify(dir + ":" + dir)
	if !es[1].Has(IssueDuplicate) {
		t.Fatalf("duplicate not flagged: %+v", es[1])
	}
	if es[1].DupOf != 0 {
		t.Fatalf("DupOf = %d, want 0", es[1].DupOf)
	}
	if es[0].Has(IssueDuplicate) {
		t.Fatal("first occurrence must not be flagged")
	}
	// "/x/bin/" and "/x/bin" are the same directory; Clean-level
	// comparison must catch the trailing slash.
	es = Classify(dir + ":" + dir + string(filepath.Separator))
	if !es[1].Has(IssueDuplicate) {
		t.Fatalf("trailing-slash duplicate not flagged: %+v", es[1])
	}
}

func TestClassifySymlinkDuplicate(t *testing.T) {
	root := t.TempDir()
	real := mkdir(t, root, "real")
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	es := Classify(real + ":" + link)
	if !es[1].Has(IssueSymlinkDuplicate) {
		t.Fatalf("symlink duplicate not flagged: %+v", es[1])
	}
	if es[1].DupOf != 0 {
		t.Fatalf("DupOf = %d, want 0", es[1].DupOf)
	}
}

func TestClassifyDistinctDirsAreNotDuplicates(t *testing.T) {
	root := t.TempDir()
	a := mkdir(t, root, "a")
	b := mkdir(t, root, "b")
	for _, e := range Classify(a + ":" + b) {
		if e.Has(IssueDuplicate) || e.Has(IssueSymlinkDuplicate) {
			t.Fatalf("distinct dir flagged as duplicate: %+v", e)
		}
	}
}

func TestClassifyWritabilityBits(t *testing.T) {
	// World-writable is a real hazard; group-writable is common
	// (staff-managed dirs) and flagging it would be pure noise.
	dir := mkdir(t, t.TempDir(), "w")
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if e := Classify(dir)[0]; !e.Has(IssueWorldWritable) {
		t.Fatalf("world-writable dir not flagged: %+v", e)
	}
	grp := mkdir(t, t.TempDir(), "g")
	if err := os.Chmod(grp, 0o775); err != nil {
		t.Fatal(err)
	}
	if e := Classify(grp)[0]; e.Has(IssueWorldWritable) {
		t.Fatalf("group-writable dir wrongly flagged: %+v", e)
	}
}

func TestClassifyUnreadableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := mkdir(t, t.TempDir(), "sealed")
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if e := Classify(dir)[0]; !e.Has(IssueUnreadable) {
		t.Fatalf("unreadable dir not flagged: %+v", e)
	}
}

func TestSeverityMapping(t *testing.T) {
	// Hazards that change what runs are errors; cruft is a warning.
	errors := []IssueKind{IssueEmpty, IssueRelative, IssueTilde, IssueWorldWritable}
	warns := []IssueKind{IssueDead, IssueNotDir, IssueDuplicate, IssueSymlinkDuplicate, IssueUnreadable}
	for _, k := range errors {
		if k.Severity() != Error {
			t.Fatalf("%s severity = %s, want error", k, k.Severity())
		}
	}
	for _, k := range warns {
		if k.Severity() != Warn {
			t.Fatalf("%s severity = %s, want warn", k, k.Severity())
		}
	}
}

func TestDescribeIsActionable(t *testing.T) {
	// The empty-segment hazard must be explained, and duplicates must
	// point back at the surviving entry (1-based, as rendered).
	e := Classify(":")[0]
	if msg := IssueEmpty.Describe(e); !strings.Contains(msg, "current directory") {
		t.Fatalf("empty-segment description must mention the current directory, got %q", msg)
	}
	dir := mkdir(t, t.TempDir(), "bin")
	es := Classify(dir + ":" + dir)
	if msg := IssueDuplicate.Describe(es[1]); !strings.Contains(msg, "entry 1") {
		t.Fatalf("duplicate description should reference entry 1, got %q", msg)
	}
}

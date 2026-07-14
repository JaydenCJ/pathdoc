// Tests for the PATH directory scanner. All fixtures are fabricated
// under t.TempDir(); executability is set explicitly with Chmod so the
// suite is independent of the host umask.
package scan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JaydenCJ/pathdoc/internal/pathenv"
)

func writeFile(t *testing.T, path, content string, mode os.FileMode) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeExec(t *testing.T, path string) string {
	t.Helper()
	return writeFile(t, path, "#!/bin/sh\nexit 0\n", 0o755)
}

func buildIndex(t *testing.T, pathStr string) *Index {
	t.Helper()
	return Build(pathenv.Classify(pathStr))
}

func TestBuildFindsExecutablesInPathOrder(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	writeExec(t, filepath.Join(a, "tool"))
	writeExec(t, filepath.Join(b, "tool"))
	idx := buildIndex(t, a+":"+b)
	bins := idx.Lookup("tool")
	if len(bins) != 2 {
		t.Fatalf("want 2 providers, got %d", len(bins))
	}
	if bins[0].Dir != a || bins[1].Dir != b {
		t.Fatalf("providers out of PATH order: %q then %q", bins[0].Dir, bins[1].Dir)
	}
	if bins[0].EntryIndex != 0 || bins[1].EntryIndex != 1 {
		t.Fatalf("entry indexes wrong: %d, %d", bins[0].EntryIndex, bins[1].EntryIndex)
	}
}

func TestBuildIndexesOnlyRealExecutables(t *testing.T) {
	// Plain files, directories, and dangling symlinks all live in bin
	// dirs in practice; none of them can be executed by the shell.
	dir := filepath.Join(t.TempDir(), "bin")
	writeFile(t, filepath.Join(dir, "README"), "docs", 0o644)
	writeExec(t, filepath.Join(dir, "tool"))
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "broken")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	idx := buildIndex(t, dir)
	for _, name := range []string{"README", "subdir", "broken"} {
		if idx.Lookup(name) != nil {
			t.Fatalf("%q must not be indexed", name)
		}
	}
	if idx.Lookup("tool") == nil {
		t.Fatal("executable missing from index")
	}
}

func TestBuildFollowsSymlinkToExecutable(t *testing.T) {
	root := t.TempDir()
	real := writeExec(t, filepath.Join(root, "store", "tool-1.2"))
	dir := filepath.Join(root, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(dir, "tool")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	bins := buildIndex(t, dir).Lookup("tool")
	if len(bins) != 1 {
		t.Fatalf("want 1 provider, got %d", len(bins))
	}
	if bins[0].Resolved != real {
		t.Fatalf("Resolved = %q, want %q", bins[0].Resolved, real)
	}
}

func TestBuildScansDuplicateDirectoryOnlyOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bin")
	writeExec(t, filepath.Join(dir, "tool"))
	idx := buildIndex(t, dir+":"+dir)
	if got := len(idx.Lookup("tool")); got != 1 {
		t.Fatalf("duplicate dir scanned twice: %d providers", got)
	}
	if idx.DirsScanned != 1 {
		t.Fatalf("DirsScanned = %d, want 1", idx.DirsScanned)
	}
}

func TestBuildSkipsDeadAndRelativeEntries(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bin")
	writeExec(t, filepath.Join(dir, "tool"))
	idx := buildIndex(t, "relative/bin:"+filepath.Join(t.TempDir(), "gone")+":"+dir)
	if idx.DirsScanned != 1 {
		t.Fatalf("DirsScanned = %d, want 1 (only the live dir)", idx.DirsScanned)
	}
}

func TestNamesAreSortedAndUnknownLookupIsNil(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bin")
	for _, n := range []string{"zeta", "alpha", "mid"} {
		writeExec(t, filepath.Join(dir, n))
	}
	idx := buildIndex(t, dir)
	want := []string{"alpha", "mid", "zeta"}
	for i, n := range want {
		if idx.Names[i] != n {
			t.Fatalf("Names[%d] = %q, want %q (deterministic order)", i, idx.Names[i], n)
		}
	}
	if got := idx.Lookup("nope"); got != nil {
		t.Fatalf("unknown name should be nil, got %v", got)
	}
}

func TestSameFileSemantics(t *testing.T) {
	// Same file through a symlink or hardlink means the shadowing is
	// benign; two independent files never compare equal.
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	c := filepath.Join(root, "c")
	d := filepath.Join(root, "d")
	real := writeExec(t, filepath.Join(a, "tool"))
	for _, dir := range []string{b, c} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(real, filepath.Join(b, "tool")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Link(real, filepath.Join(c, "tool")); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}
	writeExec(t, filepath.Join(d, "tool"))
	bins := buildIndex(t, a+":"+b+":"+c+":"+d).Lookup("tool")
	if len(bins) != 4 {
		t.Fatalf("want 4 providers, got %d", len(bins))
	}
	if !bins[0].SameFile(bins[1]) {
		t.Fatal("symlink to the winner must compare as the same file")
	}
	if !bins[0].SameFile(bins[2]) {
		t.Fatal("hardlink to the winner must compare as the same file")
	}
	if bins[0].SameFile(bins[3]) {
		t.Fatal("distinct files must not compare as the same file")
	}
}

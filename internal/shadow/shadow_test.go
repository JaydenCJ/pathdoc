// Tests for shadowing analysis: winner selection, benign (same-file)
// classification, and deterministic ordering.
package shadow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JaydenCJ/pathdoc/internal/pathenv"
	"github.com/JaydenCJ/pathdoc/internal/scan"
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

func analyze(t *testing.T, pathStr string) []Conflict {
	t.Helper()
	return Analyze(scan.Build(pathenv.Classify(pathStr)))
}

func TestSingleProviderIsNoConflict(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bin")
	writeExec(t, filepath.Join(dir, "solo"))
	if got := analyze(t, dir); len(got) != 0 {
		t.Fatalf("solo command produced conflicts: %+v", got)
	}
}

func TestFirstEntryWins(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	writeExec(t, filepath.Join(a, "tool"))
	writeExec(t, filepath.Join(b, "tool"))
	cs := analyze(t, a+":"+b)
	if len(cs) != 1 {
		t.Fatalf("want 1 conflict, got %d", len(cs))
	}
	ps := cs[0].Providers
	if !ps[0].Wins || ps[0].Dir != a {
		t.Fatalf("winner should be the first PATH entry, got %+v", ps[0])
	}
	if ps[1].Wins {
		t.Fatal("second provider must not win")
	}
}

func TestDistinctFilesAreNotBenign(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	writeExec(t, filepath.Join(a, "python3"))
	writeExec(t, filepath.Join(b, "python3"))
	cs := analyze(t, a+":"+b)
	if cs[0].Benign {
		t.Fatal("two distinct files must not be classified benign")
	}
	if cs[0].Distinct() != 1 {
		t.Fatalf("Distinct() = %d, want 1", cs[0].Distinct())
	}
}

func TestSameFileLosersAreBenign(t *testing.T) {
	// Symlink farms and usr-merge layouts shadow constantly, but always
	// with the same underlying file — those conflicts must be benign.
	root := t.TempDir()
	a, b, c := filepath.Join(root, "a"), filepath.Join(root, "b"), filepath.Join(root, "c")
	real := writeExec(t, filepath.Join(a, "node"))
	for _, dir := range []string{b, c} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(real, filepath.Join(b, "node")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Link(real, filepath.Join(c, "node")); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}
	cs := analyze(t, a+":"+b+":"+c)
	if !cs[0].Benign {
		t.Fatal("symlink and hardlink to the winner must be benign")
	}
	if !cs[0].Providers[1].SameAsWinner || !cs[0].Providers[2].SameAsWinner {
		t.Fatal("both losers should be marked SameAsWinner")
	}
}

func TestMixedLosersAreNotBenign(t *testing.T) {
	// One benign copy plus one genuinely different file: the conflict as
	// a whole must stay non-benign or the different file would be hidden.
	root := t.TempDir()
	a, b, c := filepath.Join(root, "a"), filepath.Join(root, "b"), filepath.Join(root, "c")
	real := writeExec(t, filepath.Join(a, "tool"))
	if err := os.MkdirAll(b, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(b, "tool")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeExec(t, filepath.Join(c, "tool"))
	cs := analyze(t, a+":"+b+":"+c)
	if cs[0].Benign {
		t.Fatal("a distinct third provider must keep the conflict non-benign")
	}
	if cs[0].Distinct() != 1 {
		t.Fatalf("Distinct() = %d, want 1 (the symlink loser is same-file)", cs[0].Distinct())
	}
}

func TestConflictsSortedByName(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	for _, n := range []string{"zsh-helper", "awk-helper"} {
		writeExec(t, filepath.Join(a, n))
		writeExec(t, filepath.Join(b, n))
	}
	cs := analyze(t, a+":"+b)
	if len(cs) != 2 || cs[0].Name != "awk-helper" || cs[1].Name != "zsh-helper" {
		t.Fatalf("conflicts not sorted by name: %+v", cs)
	}
}

func TestOnlyDistinctFiltersBenign(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	real := writeExec(t, filepath.Join(a, "same"))
	writeExec(t, filepath.Join(a, "diff"))
	writeExec(t, filepath.Join(b, "diff"))
	if err := os.MkdirAll(b, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(b, "same")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	kept := OnlyDistinct(analyze(t, a+":"+b))
	if len(kept) != 1 || kept[0].Name != "diff" {
		t.Fatalf("OnlyDistinct should keep just %q, got %+v", "diff", kept)
	}
}

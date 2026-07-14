// Package scan walks the live PATH directories and builds the command
// index that shadowing analysis and `pathdoc which` run on. Only the
// first occurrence of each directory is scanned — later duplicates can
// never win a lookup — and only real executables count: regular files
// (or symlinks resolving to regular files) with at least one execute bit.
package scan

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/JaydenCJ/pathdoc/internal/pathenv"
)

// Binary is one executable found in a PATH directory.
type Binary struct {
	Name       string // command name (file basename)
	Dir        string // clean PATH entry it came from
	EntryIndex int    // 0-based index into the classified entries
	Path       string // Dir joined with Name
	Resolved   string // symlink-resolved target, "" if resolution failed
	info       os.FileInfo
}

// SameFile reports whether two binaries are the same underlying file —
// via hardlink/inode identity when available, or identical symlink
// resolution otherwise. Shadowing by the same file is benign.
func (b Binary) SameFile(o Binary) bool {
	if b.info != nil && o.info != nil && os.SameFile(b.info, o.info) {
		return true
	}
	return b.Resolved != "" && b.Resolved == o.Resolved
}

// Index maps every command name to its providers in PATH order.
type Index struct {
	ByName      map[string][]Binary
	Names       []string // all command names, sorted for determinism
	DirsScanned int
}

// Lookup returns the providers of a command in PATH order (nil if none).
func (i *Index) Lookup(name string) []Binary { return i.ByName[name] }

// Scannable reports whether a classified entry contributes commands to
// lookups. Duplicates are skipped (their first occurrence already won),
// as are entries that cannot possibly be searched.
func Scannable(e pathenv.Entry) bool {
	if !e.IsDir {
		return false
	}
	for _, k := range e.Issues {
		switch k {
		case pathenv.IssueDuplicate, pathenv.IssueSymlinkDuplicate, pathenv.IssueUnreadable:
			return false
		}
	}
	return true
}

// Build scans every scannable entry in PATH order and indexes the
// executables it finds. Directory read errors and dangling symlinks are
// skipped silently: the shell skips them too.
func Build(entries []pathenv.Entry) *Index {
	idx := &Index{ByName: map[string][]Binary{}}
	for _, e := range entries {
		if !Scannable(e) {
			continue
		}
		dirents, err := os.ReadDir(e.Clean)
		if err != nil {
			continue
		}
		idx.DirsScanned++
		for _, de := range dirents {
			name := de.Name()
			full := filepath.Join(e.Clean, name)
			fi, err := os.Stat(full) // follows symlinks
			if err != nil {
				continue // dangling symlink or race — not executable
			}
			if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
				continue
			}
			b := Binary{Name: name, Dir: e.Clean, EntryIndex: e.Index, Path: full, info: fi}
			if res, err := filepath.EvalSymlinks(full); err == nil {
				b.Resolved = res
			}
			idx.ByName[name] = append(idx.ByName[name], b)
		}
	}
	idx.Names = make([]string, 0, len(idx.ByName))
	for name := range idx.ByName {
		idx.Names = append(idx.Names, name)
	}
	sort.Strings(idx.Names)
	return idx
}

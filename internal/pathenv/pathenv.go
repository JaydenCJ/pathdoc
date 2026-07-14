// Package pathenv splits a PATH string and classifies every entry:
// duplicates (textual and symlink-level), dead directories, empty and
// relative segments, unexpanded tildes, and insecure permissions.
// Classification touches the filesystem only through stat/open calls,
// so tests drive it with fabricated directory trees.
package pathenv

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// IssueKind identifies one problem with a PATH entry.
type IssueKind string

const (
	// IssueEmpty: an empty segment ("::", leading or trailing ":") makes
	// the shell search the current directory — a classic hijack vector.
	IssueEmpty IssueKind = "empty"
	// IssueRelative: the entry is resolved against $PWD, so what runs
	// depends on where you happen to be standing.
	IssueRelative IssueKind = "relative"
	// IssueTilde: command lookup never expands ~, so the entry names a
	// directory literally called "~". Almost always a quoting mistake.
	IssueTilde IssueKind = "tilde"
	// IssueDead: the directory does not exist.
	IssueDead IssueKind = "dead"
	// IssueNotDir: the entry exists but is a file, not a directory.
	IssueNotDir IssueKind = "not-dir"
	// IssueUnreadable: the directory cannot be listed; its commands are
	// invisible to this scan but may still execute.
	IssueUnreadable IssueKind = "unreadable"
	// IssueDuplicate: textually identical to an earlier entry.
	IssueDuplicate IssueKind = "duplicate"
	// IssueSymlinkDuplicate: resolves (through symlinks) to the same
	// directory as an earlier entry.
	IssueSymlinkDuplicate IssueKind = "symlink-duplicate"
	// IssueWorldWritable: any local user can plant a binary here that
	// shadows every later entry.
	IssueWorldWritable IssueKind = "world-writable"
)

// Severity buckets issues for `pathdoc check`.
type Severity int

const (
	Info Severity = iota
	Warn
	Error
)

func (s Severity) String() string {
	switch s {
	case Error:
		return "error"
	case Warn:
		return "warn"
	default:
		return "info"
	}
}

// Severity returns how serious an issue kind is. Hazards that can change
// which binary runs (or let another user decide) are errors; cruft that
// merely wastes lookups is a warning.
func (k IssueKind) Severity() Severity {
	switch k {
	case IssueEmpty, IssueRelative, IssueTilde, IssueWorldWritable:
		return Error
	default:
		return Warn
	}
}

// Describe renders a full explanation of the issue for entry e. The
// caller prefixes it with "entry N ".
func (k IssueKind) Describe(e Entry) string {
	switch k {
	case IssueEmpty:
		return "is empty — the shell treats an empty segment as the current directory, so any file in $PWD named like a command wins"
	case IssueRelative:
		return fmt.Sprintf("(%s) is relative — it resolves against whatever the current directory happens to be", e.Raw)
	case IssueTilde:
		return fmt.Sprintf("(%s) has an unexpanded ~ — command lookup does not expand it, so a directory literally named \"~\" is searched", e.Raw)
	case IssueDead:
		return fmt.Sprintf("(%s) does not exist", e.Clean)
	case IssueNotDir:
		return fmt.Sprintf("(%s) is not a directory", e.Clean)
	case IssueUnreadable:
		return fmt.Sprintf("(%s) cannot be read — its commands are invisible to this scan but may still run", e.Clean)
	case IssueDuplicate:
		return fmt.Sprintf("(%s) duplicates entry %d and is never consulted", e.Clean, e.DupOf+1)
	case IssueSymlinkDuplicate:
		return fmt.Sprintf("(%s) resolves to the same directory as entry %d", e.Clean, e.DupOf+1)
	case IssueWorldWritable:
		return fmt.Sprintf("(%s) is world-writable — any local user can plant a binary that shadows every later entry", e.Clean)
	}
	return string(k)
}

// Entry is one classified PATH segment, in original order.
type Entry struct {
	Index    int    // 0-based position in PATH
	Raw      string // the segment exactly as it appears in PATH
	Clean    string // filepath.Clean(Raw); "" for empty segments
	Resolved string // symlink-resolved directory, "" if resolution failed
	Exists   bool
	IsDir    bool
	DupOf    int // 0-based index of the first equivalent entry, -1 if none
	Issues   []IssueKind
}

// OK reports whether the entry has no issues at all.
func (e Entry) OK() bool { return len(e.Issues) == 0 }

// Has reports whether the entry carries a specific issue kind.
func (e Entry) Has(k IssueKind) bool {
	for _, i := range e.Issues {
		if i == k {
			return true
		}
	}
	return false
}

// MaxSeverity returns the most serious severity among the entry's issues,
// or Info when the entry is clean.
func (e Entry) MaxSeverity() Severity {
	max := Info
	for _, k := range e.Issues {
		if s := k.Severity(); s > max {
			max = s
		}
	}
	return max
}

func (e *Entry) add(k IssueKind) { e.Issues = append(e.Issues, k) }

// Split divides a PATH string into its segments, preserving empty ones
// (they are meaningful: an empty segment means the current directory).
// An entirely empty PATH yields no segments.
func Split(pathStr string) []string {
	if pathStr == "" {
		return nil
	}
	return strings.Split(pathStr, string(os.PathListSeparator))
}

// Classify splits pathStr and diagnoses every entry in order.
func Classify(pathStr string) []Entry {
	segs := Split(pathStr)
	entries := make([]Entry, len(segs))
	seenClean := map[string]int{}
	seenResolved := map[string]int{}
	for i, raw := range segs {
		e := Entry{Index: i, Raw: raw, DupOf: -1}
		switch {
		case raw == "":
			e.add(IssueEmpty)
		case raw == "~" || strings.HasPrefix(raw, "~/"):
			e.Clean = raw
			e.add(IssueTilde)
		case !filepath.IsAbs(raw):
			e.Clean = filepath.Clean(raw)
			e.add(IssueRelative)
		default:
			e.Clean = filepath.Clean(raw)
			statEntry(&e)
			if j, ok := seenClean[e.Clean]; ok {
				e.DupOf = j
				e.add(IssueDuplicate)
			} else {
				seenClean[e.Clean] = i
				if e.Resolved != "" {
					if j, ok := seenResolved[e.Resolved]; ok {
						e.DupOf = j
						e.add(IssueSymlinkDuplicate)
					} else {
						seenResolved[e.Resolved] = i
					}
				}
			}
		}
		entries[i] = e
	}
	return entries
}

// statEntry fills in existence, directory-ness, permissions, and the
// symlink-resolved form of an absolute entry.
func statEntry(e *Entry) {
	fi, err := os.Stat(e.Clean)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			e.add(IssueUnreadable)
		} else {
			e.add(IssueDead)
		}
		return
	}
	e.Exists = true
	if !fi.IsDir() {
		e.add(IssueNotDir)
		return
	}
	e.IsDir = true
	if fi.Mode().Perm()&0o002 != 0 {
		e.add(IssueWorldWritable)
	}
	if res, err := filepath.EvalSymlinks(e.Clean); err == nil {
		e.Resolved = res
	}
	// Probe listability: a directory without read permission still lets
	// the kernel execute known names, but this scan cannot see inside.
	if f, err := os.Open(e.Clean); err != nil {
		e.add(IssueUnreadable)
	} else {
		f.Close()
	}
}

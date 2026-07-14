// Package report renders the analysis model as human text and stable
// JSON. Rendering is pure: the model is assembled by the CLI layer and
// every function here writes deterministically ordered output, so tests
// can assert on exact fragments.
package report

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/JaydenCJ/pathdoc/internal/pathenv"
	"github.com/JaydenCJ/pathdoc/internal/provenance"
	"github.com/JaydenCJ/pathdoc/internal/rcparse"
	"github.com/JaydenCJ/pathdoc/internal/scan"
	"github.com/JaydenCJ/pathdoc/internal/shadow"
)

// Model is everything one pathdoc run learned.
type Model struct {
	Path      string
	Entries   []pathenv.Entry
	Index     *scan.Index
	Conflicts []shadow.Conflict
	Prov      *provenance.Result // nil when provenance scanning is disabled
	Home      string
}

// Abbrev shortens absolute paths under home to ~/… for text output.
func Abbrev(p, home string) string {
	if home == "" || p == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

// displayEntry renders a PATH entry for humans.
func displayEntry(e pathenv.Entry, home string) string {
	if e.Raw == "" {
		return "(empty)"
	}
	return Abbrev(e.Raw, home)
}

// shortStatus is the compact status cell for the entries table.
func shortStatus(e pathenv.Entry) string {
	if e.OK() {
		return "ok"
	}
	parts := make([]string, 0, len(e.Issues))
	for _, k := range e.Issues {
		switch k {
		case pathenv.IssueDuplicate:
			parts = append(parts, fmt.Sprintf("duplicate of #%d", e.DupOf+1))
		case pathenv.IssueSymlinkDuplicate:
			parts = append(parts, fmt.Sprintf("same dir as #%d (symlink)", e.DupOf+1))
		case pathenv.IssueDead:
			parts = append(parts, "does not exist")
		case pathenv.IssueNotDir:
			parts = append(parts, "not a directory")
		case pathenv.IssueEmpty:
			parts = append(parts, "empty = current dir")
		case pathenv.IssueRelative:
			parts = append(parts, "relative")
		case pathenv.IssueTilde:
			parts = append(parts, "unexpanded ~")
		case pathenv.IssueWorldWritable:
			parts = append(parts, "world-writable")
		case pathenv.IssueUnreadable:
			parts = append(parts, "unreadable")
		default:
			parts = append(parts, string(k))
		}
	}
	return strings.Join(parts, ", ")
}

// refLabel renders one provenance ref: "~/.zshrc:12 · pyenv hook".
func refLabel(r provenance.Ref, home string) string {
	loc := fmt.Sprintf("%s:%d", Abbrev(r.File, home), r.Line)
	switch {
	case r.Op == rcparse.OpHook:
		loc += " · " + r.Tool + " hook"
	case r.Tool != "":
		loc += " · " + r.Tool
	}
	if len(r.Via) > 0 {
		loc += fmt.Sprintf(" (via %s)", Abbrev(r.Via[0], home))
	}
	return loc
}

// provCell is the provenance column for the entries table.
func provCell(m *Model, e pathenv.Entry) string {
	refs := m.Prov.Refs[e.Index]
	if len(refs) == 0 {
		if len(provenance.Candidates(e)) == 0 {
			return "—"
		}
		return "(inherited — no rc line found)"
	}
	cell := refLabel(refs[0], m.Home)
	if len(refs) > 1 {
		cell += fmt.Sprintf(" (+%d more)", len(refs)-1)
	}
	return cell
}

// provSentence is the long-form provenance used by which/shadows.
func (m *Model) provSentence(idx int) string {
	if m.Prov == nil {
		return ""
	}
	var e pathenv.Entry
	for _, cand := range m.Entries {
		if cand.Index == idx {
			e = cand
			break
		}
	}
	refs := m.Prov.Refs[idx]
	if len(refs) == 0 {
		if len(provenance.Candidates(e)) == 0 {
			return ""
		}
		return "no rc line claims this entry (inherited)"
	}
	s := "added by " + refLabel(refs[0], m.Home)
	if len(refs) > 1 {
		s += fmt.Sprintf(" (+%d more)", len(refs)-1)
	}
	return s
}

// Finding is one issue expressed as a check-able sentence.
type Finding struct {
	Severity pathenv.Severity
	Text     string
}

// Findings flattens entry issues and shadowing conflicts into the list
// `pathdoc check` gates on. Entry issues keep their kind's severity;
// a conflict over different files is a warning, a benign one is info.
func Findings(m *Model) []Finding {
	var out []Finding
	for _, e := range m.Entries {
		for _, k := range e.Issues {
			out = append(out, Finding{
				Severity: k.Severity(),
				Text:     fmt.Sprintf("entry %d %s", e.Index+1, k.Describe(e)),
			})
		}
	}
	for _, c := range m.Conflicts {
		w := c.Providers[0]
		if c.Benign {
			out = append(out, Finding{
				Severity: pathenv.Info,
				Text: fmt.Sprintf("%s has %d providers, all the same file (benign)",
					c.Name, len(c.Providers)),
			})
			continue
		}
		out = append(out, Finding{
			Severity: pathenv.Warn,
			Text: fmt.Sprintf("%s is shadowed: %s hides %d other candidate(s), %d different file(s)",
				c.Name, Abbrev(w.Path, m.Home), len(c.Providers)-1, c.Distinct()),
		})
	}
	return out
}

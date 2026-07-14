package report

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/JaydenCJ/pathdoc/internal/provenance"
	"github.com/JaydenCJ/pathdoc/internal/rcparse"
	"github.com/JaydenCJ/pathdoc/internal/shadow"
)

// Text renders the full diagnosis: header, entries table, issues, and a
// shadowing summary.
func Text(w io.Writer, m *Model) {
	clean, issueCount := 0, 0
	for _, e := range m.Entries {
		if e.OK() {
			clean++
		}
		issueCount += len(e.Issues)
	}
	benign := 0
	for _, c := range m.Conflicts {
		if c.Benign {
			benign++
		}
	}
	fmt.Fprintf(w, "pathdoc report — %d entries · %d clean · %d issue(s) · %d shadowing conflict(s)",
		len(m.Entries), clean, issueCount, len(m.Conflicts))
	if benign > 0 {
		fmt.Fprintf(w, " (%d benign)", benign)
	}
	fmt.Fprintln(w)
	if m.Prov != nil {
		opaque := 0
		for _, mu := range m.Prov.Mutations {
			if mu.Op == rcparse.OpOpaque {
				opaque++
			}
		}
		fmt.Fprintf(w, "provenance: %d rc file(s) scanned · %d PATH-modifying line(s)",
			len(m.Prov.FilesRead), len(m.Prov.Mutations))
		if opaque > 0 {
			fmt.Fprintf(w, " (%d opaque)", opaque)
		}
		fmt.Fprintln(w)
	}
	if len(m.Entries) == 0 {
		fmt.Fprintln(w, "\nPATH is empty — nothing to diagnose.")
		return
	}

	fmt.Fprintln(w)
	writeEntriesTable(w, m)

	fmt.Fprintln(w)
	writeIssues(w, m)

	fmt.Fprintln(w)
	writeShadowSummary(w, m)
}

func writeEntriesTable(w io.Writer, m *Model) {
	header := []string{"#", "entry", "status"}
	if m.Prov != nil {
		header = append(header, "provenance")
	}
	rows := [][]string{header}
	for _, e := range m.Entries {
		row := []string{strconv.Itoa(e.Index + 1), displayEntry(e, m.Home), shortStatus(e)}
		if m.Prov != nil {
			row = append(row, provCell(m, e))
		}
		rows = append(rows, row)
	}
	widths := make([]int, len(header))
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	for _, row := range rows {
		fmt.Fprintf(w, " %*s", widths[0], row[0])
		for i := 1; i < len(row); i++ {
			if i == len(row)-1 {
				fmt.Fprintf(w, "  %s", row[i])
			} else {
				fmt.Fprintf(w, "  %-*s", widths[i], row[i])
			}
		}
		fmt.Fprintln(w)
	}
}

func writeIssues(w io.Writer, m *Model) {
	var lines []string
	for _, e := range m.Entries {
		for _, k := range e.Issues {
			lines = append(lines, fmt.Sprintf("  [%-5s] entry %d %s", k.Severity(), e.Index+1, k.Describe(e)))
		}
	}
	if len(lines) == 0 {
		fmt.Fprintln(w, "no issues found.")
		return
	}
	fmt.Fprintln(w, "issues")
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
}

func writeShadowSummary(w io.Writer, m *Model) {
	if len(m.Conflicts) == 0 {
		fmt.Fprintln(w, "no shadowing conflicts.")
		return
	}
	fmt.Fprintln(w, "shadowing")
	nameW := 0
	for _, c := range m.Conflicts {
		if len(c.Name) > nameW {
			nameW = len(c.Name)
		}
	}
	for _, c := range m.Conflicts {
		winner := c.Providers[0]
		note := fmt.Sprintf("%d shadowed · %d different file(s)", len(c.Providers)-1, c.Distinct())
		if c.Benign {
			note = fmt.Sprintf("%d shadowed · same file — benign", len(c.Providers)-1)
		}
		fmt.Fprintf(w, "  %-*s  %s wins · %s\n", nameW, c.Name, Abbrev(winner.Path, m.Home), note)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "run `pathdoc which <command>` to see every candidate with provenance.")
}

// WhichText renders every candidate for each name, winner first, and
// returns how many names were not found at all.
func WhichText(w io.Writer, m *Model, names []string) int {
	missing := 0
	for ni, name := range names {
		if ni > 0 {
			fmt.Fprintln(w)
		}
		bins := m.Index.Lookup(name)
		if len(bins) == 0 {
			fmt.Fprintf(w, "%s: not found on PATH\n", name)
			missing++
			continue
		}
		fmt.Fprintf(w, "%s — %d candidate(s) on PATH, first wins\n\n", name, len(bins))
		for i, b := range bins {
			marker, verdict := "  ", "wins"
			if i == 0 {
				marker = "► "
			} else if b.SameFile(bins[0]) {
				verdict = "shadowed — same file as winner"
			} else {
				verdict = "shadowed — different file"
			}
			fmt.Fprintf(w, "  %s%s    (%s)\n", marker, Abbrev(b.Path, m.Home), verdict)
			detail := fmt.Sprintf("entry %d", b.EntryIndex+1)
			if s := m.provSentence(b.EntryIndex); s != "" {
				detail += " · " + s
			}
			fmt.Fprintf(w, "      %s\n", detail)
			if b.Resolved != "" && b.Resolved != b.Path {
				fmt.Fprintf(w, "      symlink → %s\n", Abbrev(b.Resolved, m.Home))
			}
		}
	}
	return missing
}

// ShadowsText renders the conflict list; benign conflicts are hidden
// unless all is set.
func ShadowsText(w io.Writer, m *Model, all bool) {
	shown := m.Conflicts
	if !all {
		shown = shadow.OnlyDistinct(m.Conflicts)
	}
	hidden := len(m.Conflicts) - len(shown)
	fmt.Fprintf(w, "%d shadowing conflict(s)", len(shown))
	if hidden > 0 {
		fmt.Fprintf(w, " (%d benign hidden — use --all to show)", hidden)
	}
	fmt.Fprintln(w)
	for _, c := range shown {
		fmt.Fprintf(w, "\n%s — %d candidates\n", c.Name, len(c.Providers))
		for _, p := range c.Providers {
			marker, verdict := "  ", "wins"
			switch {
			case p.Wins:
				marker = "► "
			case p.SameAsWinner:
				verdict = "shadowed — same file as winner"
			default:
				verdict = "shadowed — different file"
			}
			fmt.Fprintf(w, "  %s%s    (%s)\n", marker, Abbrev(p.Path, m.Home), verdict)
			detail := fmt.Sprintf("entry %d", p.EntryIndex+1)
			if s := m.provSentence(p.EntryIndex); s != "" {
				detail += " · " + s
			}
			fmt.Fprintf(w, "      %s\n", detail)
		}
	}
}

// RCText renders every PATH-modifying rc line in startup order, with
// the entries each one explains.
func RCText(w io.Writer, m *Model) {
	if m.Prov == nil {
		fmt.Fprintln(w, "provenance scanning is disabled (--no-provenance).")
		return
	}
	fmt.Fprintf(w, "%d PATH-modifying line(s) from %d rc file(s), in startup order\n",
		len(m.Prov.Mutations), len(m.Prov.FilesRead))
	for _, mu := range m.Prov.Mutations {
		head := fmt.Sprintf("%s:%d  [%s]", Abbrev(mu.File, m.Home), mu.Line, mu.Op)
		if mu.Tool != "" {
			head += " " + mu.Tool
		}
		if len(mu.Via) > 0 {
			head += fmt.Sprintf("  (sourced via %s)", Abbrev(mu.Via[0], m.Home))
		}
		fmt.Fprintf(w, "\n%s\n", head)
		fmt.Fprintf(w, "  └─ %s\n", mu.Text)
		if mu.Op == rcparse.OpOpaque {
			fmt.Fprintln(w, "     (cannot be statically parsed)")
			continue
		}
		writeSegList(w, m, "prepends", mu.Prepends)
		writeSegList(w, m, "appends", mu.Appends)
	}
}

func writeSegList(w io.Writer, m *Model, label string, segs []rcparse.Segment) {
	if len(segs) == 0 {
		return
	}
	parts := make([]string, 0, len(segs))
	for _, s := range segs {
		p := Abbrev(s.Expanded, m.Home)
		if s.Unresolved {
			p = s.Raw + " (unresolved)"
		}
		if idx, ok := matchedEntry(m, s); ok {
			p += fmt.Sprintf(" (= entry %d)", idx+1)
		}
		parts = append(parts, p)
	}
	fmt.Fprintf(w, "     %s: %s\n", label, joinComma(parts))
}

func matchedEntry(m *Model, s rcparse.Segment) (int, bool) {
	for _, e := range m.Entries {
		if provenance.MatchSegment(s, provenance.Candidates(e)) {
			return e.Index, true
		}
	}
	return 0, false
}

func joinComma(parts []string) string { return strings.Join(parts, ", ") }

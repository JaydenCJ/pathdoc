package report

import (
	"encoding/json"
	"io"

	"github.com/JaydenCJ/pathdoc/internal/pathenv"
	"github.com/JaydenCJ/pathdoc/internal/provenance"
)

// SchemaVersion identifies the JSON envelope; bump only on breaking
// changes to field names or semantics.
const SchemaVersion = 1

type jsonRef struct {
	File string   `json:"file"`
	Line int      `json:"line"`
	Text string   `json:"text"`
	Op   string   `json:"op"`
	Tool string   `json:"tool,omitempty"`
	Via  []string `json:"via,omitempty"`
}

func refsToJSON(refs []provenance.Ref) []jsonRef {
	out := make([]jsonRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, jsonRef{File: r.File, Line: r.Line, Text: r.Text, Op: string(r.Op), Tool: r.Tool, Via: r.Via})
	}
	return out
}

type jsonEntry struct {
	Index       int       `json:"index"` // 1-based, matching text output
	Raw         string    `json:"raw"`
	Clean       string    `json:"clean,omitempty"`
	Resolved    string    `json:"resolved,omitempty"`
	Exists      bool      `json:"exists"`
	Dir         bool      `json:"dir"`
	Issues      []string  `json:"issues"`
	DuplicateOf int       `json:"duplicate_of,omitempty"` // 1-based
	Provenance  []jsonRef `json:"provenance,omitempty"`
}

type jsonProvider struct {
	Path             string `json:"path"`
	Entry            int    `json:"entry"` // 1-based
	Wins             bool   `json:"wins"`
	SameFileAsWinner bool   `json:"same_file_as_winner"`
	Resolved         string `json:"resolved,omitempty"`
}

type jsonShadow struct {
	Name      string         `json:"name"`
	Benign    bool           `json:"benign"`
	Providers []jsonProvider `json:"providers"`
}

type jsonSummary struct {
	Entries         int `json:"entries"`
	Clean           int `json:"clean"`
	Issues          int `json:"issues"`
	Conflicts       int `json:"conflicts"`
	BenignConflicts int `json:"benign_conflicts"`
	DirsScanned     int `json:"dirs_scanned"`
	CommandsSeen    int `json:"commands_seen"`
}

type jsonReport struct {
	Tool          string       `json:"tool"`
	SchemaVersion int          `json:"schema_version"`
	Path          string       `json:"path"`
	Summary       jsonSummary  `json:"summary"`
	Entries       []jsonEntry  `json:"entries"`
	Shadows       []jsonShadow `json:"shadows"`
	RCFiles       []string     `json:"rc_files,omitempty"`
}

func entryToJSON(m *Model, e pathenv.Entry) jsonEntry {
	je := jsonEntry{
		Index: e.Index + 1, Raw: e.Raw, Clean: e.Clean, Resolved: e.Resolved,
		Exists: e.Exists, Dir: e.IsDir, Issues: []string{},
	}
	for _, k := range e.Issues {
		je.Issues = append(je.Issues, string(k))
	}
	if e.DupOf >= 0 {
		je.DuplicateOf = e.DupOf + 1
	}
	if m.Prov != nil {
		je.Provenance = refsToJSON(m.Prov.Refs[e.Index])
	}
	return je
}

func shadowsToJSON(m *Model) []jsonShadow {
	out := make([]jsonShadow, 0, len(m.Conflicts))
	for _, c := range m.Conflicts {
		js := jsonShadow{Name: c.Name, Benign: c.Benign}
		for _, p := range c.Providers {
			js.Providers = append(js.Providers, jsonProvider{
				Path: p.Path, Entry: p.EntryIndex + 1, Wins: p.Wins,
				SameFileAsWinner: p.SameAsWinner, Resolved: p.Resolved,
			})
		}
		out = append(out, js)
	}
	return out
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// JSON renders the full report envelope.
func JSON(w io.Writer, m *Model) error {
	rep := jsonReport{
		Tool: "pathdoc", SchemaVersion: SchemaVersion, Path: m.Path,
		Entries: []jsonEntry{}, Shadows: shadowsToJSON(m),
	}
	for _, e := range m.Entries {
		rep.Entries = append(rep.Entries, entryToJSON(m, e))
		if e.OK() {
			rep.Summary.Clean++
		}
		rep.Summary.Issues += len(e.Issues)
	}
	rep.Summary.Entries = len(m.Entries)
	rep.Summary.Conflicts = len(m.Conflicts)
	for _, c := range m.Conflicts {
		if c.Benign {
			rep.Summary.BenignConflicts++
		}
	}
	rep.Summary.DirsScanned = m.Index.DirsScanned
	rep.Summary.CommandsSeen = len(m.Index.Names)
	if m.Prov != nil {
		rep.RCFiles = m.Prov.FilesRead
	}
	return writeJSON(w, rep)
}

type jsonWhichResult struct {
	Name       string          `json:"name"`
	Found      bool            `json:"found"`
	Candidates []jsonCandidate `json:"candidates"`
}

type jsonCandidate struct {
	Path             string    `json:"path"`
	Entry            int       `json:"entry"`
	Wins             bool      `json:"wins"`
	SameFileAsWinner bool      `json:"same_file_as_winner"`
	Resolved         string    `json:"resolved,omitempty"`
	Provenance       []jsonRef `json:"provenance,omitempty"`
}

// WhichJSON renders the candidates for each queried name.
func WhichJSON(w io.Writer, m *Model, names []string) error {
	type envelope struct {
		Tool          string            `json:"tool"`
		SchemaVersion int               `json:"schema_version"`
		Results       []jsonWhichResult `json:"results"`
	}
	env := envelope{Tool: "pathdoc", SchemaVersion: SchemaVersion, Results: []jsonWhichResult{}}
	for _, name := range names {
		bins := m.Index.Lookup(name)
		res := jsonWhichResult{Name: name, Found: len(bins) > 0, Candidates: []jsonCandidate{}}
		for i, b := range bins {
			c := jsonCandidate{
				Path: b.Path, Entry: b.EntryIndex + 1, Wins: i == 0,
				SameFileAsWinner: i > 0 && b.SameFile(bins[0]), Resolved: b.Resolved,
			}
			if m.Prov != nil {
				c.Provenance = refsToJSON(m.Prov.Refs[b.EntryIndex])
			}
			res.Candidates = append(res.Candidates, c)
		}
		env.Results = append(env.Results, res)
	}
	return writeJSON(w, env)
}

// ShadowsJSON renders only the conflict list (optionally with benign).
func ShadowsJSON(w io.Writer, m *Model, all bool) error {
	type envelope struct {
		Tool          string       `json:"tool"`
		SchemaVersion int          `json:"schema_version"`
		Shadows       []jsonShadow `json:"shadows"`
	}
	env := envelope{Tool: "pathdoc", SchemaVersion: SchemaVersion, Shadows: []jsonShadow{}}
	for _, js := range shadowsToJSON(m) {
		if js.Benign && !all {
			continue
		}
		env.Shadows = append(env.Shadows, js)
	}
	return writeJSON(w, env)
}

type jsonMutation struct {
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Text     string   `json:"text"`
	Op       string   `json:"op"`
	Tool     string   `json:"tool,omitempty"`
	Via      []string `json:"via,omitempty"`
	Prepends []string `json:"prepends,omitempty"`
	Appends  []string `json:"appends,omitempty"`
	Matches  []int    `json:"matches,omitempty"` // 1-based entry indexes
}

// RCJSON renders every PATH-modifying rc line in startup order.
func RCJSON(w io.Writer, m *Model) error {
	type envelope struct {
		Tool          string         `json:"tool"`
		SchemaVersion int            `json:"schema_version"`
		RCFiles       []string       `json:"rc_files"`
		Mutations     []jsonMutation `json:"mutations"`
	}
	env := envelope{Tool: "pathdoc", SchemaVersion: SchemaVersion, RCFiles: []string{}, Mutations: []jsonMutation{}}
	if m.Prov == nil {
		return writeJSON(w, env)
	}
	env.RCFiles = append(env.RCFiles, m.Prov.FilesRead...)
	for _, mu := range m.Prov.Mutations {
		jm := jsonMutation{File: mu.File, Line: mu.Line, Text: mu.Text, Op: string(mu.Op), Tool: mu.Tool, Via: mu.Via}
		for _, s := range mu.Prepends {
			jm.Prepends = append(jm.Prepends, s.Expanded)
		}
		for _, s := range mu.Appends {
			jm.Appends = append(jm.Appends, s.Expanded)
		}
		seen := map[int]bool{}
		for _, s := range mu.Segments() {
			if idx, ok := matchedEntry(m, s); ok && !seen[idx] {
				seen[idx] = true
				jm.Matches = append(jm.Matches, idx+1)
			}
		}
		env.Mutations = append(env.Mutations, jm)
	}
	return writeJSON(w, env)
}

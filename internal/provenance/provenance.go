// Package provenance answers "which rc line added this PATH entry" by
// matching classified entries against the PATH mutations rcparse found.
// Matching is deliberately conservative: a segment matches only when its
// fully-expanded form names the entry (or the entry's symlink-resolved
// directory) exactly, or when a tool-hook glob pattern covers it.
// Unresolved variables never match, so pathdoc never guesses.
package provenance

import (
	"path/filepath"

	"github.com/JaydenCJ/pathdoc/internal/pathenv"
	"github.com/JaydenCJ/pathdoc/internal/rcparse"
)

// Ref is one rc line that plausibly added a PATH entry.
type Ref struct {
	File string
	Line int
	Text string
	Op   rcparse.Op
	Tool string
	Via  []string // source/. chain that reached the file, outermost first
}

// Result holds everything the rc scan learned, keyed for rendering.
type Result struct {
	Refs      map[int][]Ref // entry index → matching rc lines, startup order
	Mutations []rcparse.Mutation
	FilesRead []string
}

// Attribute matches every entry against every mutation, keeping all
// matches in startup order. Several lines can legitimately mention the
// same directory (e.g. a hook plus an explicit export); the renderer
// shows the first and counts the rest.
func Attribute(entries []pathenv.Entry, muts []rcparse.Mutation, filesRead []string) *Result {
	res := &Result{Refs: map[int][]Ref{}, Mutations: muts, FilesRead: filesRead}
	for _, e := range entries {
		cands := Candidates(e)
		if len(cands) == 0 {
			continue
		}
		for _, m := range muts {
			if !matchesMutation(m, cands) {
				continue
			}
			res.Refs[e.Index] = append(res.Refs[e.Index], Ref{
				File: m.File, Line: m.Line, Text: m.Text,
				Op: m.Op, Tool: m.Tool, Via: m.Via,
			})
		}
	}
	return res
}

// Candidates returns the strings an entry can be recognized by: its
// cleaned form and, when different, its symlink-resolved directory.
func Candidates(e pathenv.Entry) []string {
	var out []string
	if e.Clean != "" && filepath.IsAbs(e.Clean) {
		out = append(out, e.Clean)
	}
	if e.Resolved != "" && e.Resolved != e.Clean {
		out = append(out, e.Resolved)
	}
	return out
}

func matchesMutation(m rcparse.Mutation, cands []string) bool {
	for _, seg := range m.Segments() {
		if MatchSegment(seg, cands) {
			return true
		}
	}
	return false
}

// MatchSegment reports whether one rc segment names one of the
// candidate paths. Glob segments (from tool hooks like
// ~/.nvm/versions/node/*/bin) match via filepath.Match.
func MatchSegment(seg rcparse.Segment, cands []string) bool {
	if seg.Unresolved || seg.Expanded == "" {
		return false
	}
	for _, c := range cands {
		if seg.Glob {
			if ok, err := filepath.Match(seg.Expanded, c); err == nil && ok {
				return true
			}
		} else if seg.Expanded == c {
			return true
		}
	}
	return false
}

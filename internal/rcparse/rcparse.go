// Package rcparse statically scans shell startup files for the lines
// that modify PATH. It understands POSIX assignments (with export /
// typeset / declare prefixes), zsh path arrays, fish set and
// fish_add_path, macOS /etc/paths files, the eval hooks of common
// version managers, and it follows source / "." includes.
//
// The scan is line-level and deliberately ignores control flow: for
// provenance the question is "which line COULD have added this entry",
// not "which branch actually ran". Lines that recognizably touch PATH
// but cannot be statically resolved (command substitution, multiple
// $PATH references) are recorded as opaque instead of being guessed at.
package rcparse

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Op describes what a mutation does to PATH.
type Op string

const (
	OpReplace Op = "replace" // PATH=/a:/b — discards the inherited value
	OpPrepend Op = "prepend" // PATH=/a:$PATH
	OpAppend  Op = "append"  // PATH=$PATH:/a
	OpMixed   Op = "mixed"   // segments on both sides of $PATH
	OpHook    Op = "hook"    // eval/source hook of a known tool
	OpOpaque  Op = "opaque"  // touches PATH but cannot be statically parsed
)

// Mutation is one rc line that modifies PATH.
type Mutation struct {
	File     string
	Line     int
	Text     string // the logical source line, trimmed
	Op       Op
	Prepends []Segment // segments placed before the inherited PATH
	Appends  []Segment // segments placed after it
	Tool     string    // for OpHook and /etc/paths ("path_helper")
	Via      []string  // "file:line" chain when reached through source/.
}

// Segments returns prepends then appends as one slice.
func (m Mutation) Segments() []Segment {
	out := make([]Segment, 0, len(m.Prepends)+len(m.Appends))
	out = append(out, m.Prepends...)
	return append(out, m.Appends...)
}

// Ref renders the canonical "file:line" location of the mutation.
func (m Mutation) Ref() string { return fmt.Sprintf("%s:%d", m.File, m.Line) }

// maxSourceDepth bounds how far source/. chains are followed.
const maxSourceDepth = 5

type parser struct {
	env     Env
	visited map[string]bool
	muts    []Mutation
	read    []string
}

// Scan parses every listed file in order (missing files are skipped
// silently — rc sets are speculative by nature) and returns the
// mutations found plus the files actually read, includes included.
func Scan(files []string, env Env) (muts []Mutation, filesRead []string) {
	p := &parser{env: env, visited: map[string]bool{}}
	for _, f := range files {
		p.parseFile(f, nil)
	}
	return p.muts, p.read
}

func (p *parser) parseFile(file string, via []string) {
	if abs, err := filepath.Abs(file); err == nil {
		file = abs
	}
	if p.visited[file] {
		return
	}
	p.visited[file] = true
	data, err := os.ReadFile(file)
	if err != nil {
		return
	}
	p.read = append(p.read, file)
	pathsFormat := isPathsFile(file)
	for _, ln := range splitLines(string(data)) {
		text := strings.TrimSpace(ln.text)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		if pathsFormat {
			// macOS path_helper format: one directory per line,
			// prepended to PATH ahead of the inherited value.
			p.muts = append(p.muts, Mutation{
				File: file, Line: ln.num, Text: text, Op: OpPrepend,
				Prepends: []Segment{expandSegment(text, p.env)},
				Tool:     "path_helper", Via: via,
			})
			continue
		}
		text = stripComment(text)
		if text == "" {
			continue
		}
		p.scanHooks(file, ln.num, text, via)
		p.scanLine(file, ln.num, text, via)
		p.followSources(file, ln.num, text, via)
	}
}

// isPathsFile reports whether a file uses the macOS path_helper format.
func isPathsFile(file string) bool {
	return filepath.Base(file) == "paths" || filepath.Base(filepath.Dir(file)) == "paths.d"
}

type logicalLine struct {
	text string
	num  int // 1-based line number where the logical line starts
}

// splitLines splits source text into logical lines, joining trailing-
// backslash continuations so `export PATH=\` + `"/a:$PATH"` parses as
// one assignment. Like the shell, the backslash-newline pair is deleted
// outright — no separator is inserted.
func splitLines(s string) []logicalLine {
	raw := strings.Split(s, "\n")
	var out []logicalLine
	for i := 0; i < len(raw); i++ {
		start := i + 1
		line := raw[i]
		for strings.HasSuffix(line, "\\") && !strings.HasSuffix(line, "\\\\") && i+1 < len(raw) {
			i++
			line = line[:len(line)-1] + raw[i]
		}
		out = append(out, logicalLine{text: line, num: start})
	}
	return out
}

// stripComment cuts an unquoted trailing "# comment" off a line.
func stripComment(text string) string {
	inS, inD := false, false
	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case c == '\'' && !inD:
			inS = !inS
		case c == '"' && !inS:
			inD = !inD
		case c == '#' && !inS && !inD:
			if i == 0 || text[i-1] == ' ' || text[i-1] == '\t' {
				return strings.TrimSpace(text[:i])
			}
		}
	}
	return strings.TrimSpace(text)
}

// scanLine tries every mutation recognizer against each simple command
// on the line. If none matches but the line still visibly touches PATH,
// an opaque mutation is recorded so nothing is silently dropped.
func (p *parser) scanLine(file string, num int, text string, via []string) {
	matched := false
	for _, cmd := range splitCommands(text) {
		cmd = stripKeywords(cmd)
		if cmd == "" {
			continue
		}
		res, ok := recognize(cmd)
		if !ok {
			continue
		}
		matched = true
		if res.op == OpOpaque {
			p.muts = append(p.muts, Mutation{File: file, Line: num, Text: text, Op: OpOpaque, Via: via})
			continue
		}
		m := Mutation{File: file, Line: num, Text: text, Op: res.op, Tool: res.tool, Via: via}
		m.Prepends = p.segments(res.before, res.literal)
		m.Appends = p.segments(res.after, res.literal)
		if len(m.Prepends)+len(m.Appends) == 0 {
			continue // e.g. PATH=$PATH — a no-op
		}
		p.muts = append(p.muts, m)
	}
	if !matched && mentionsPath(text) {
		p.muts = append(p.muts, Mutation{File: file, Line: num, Text: text, Op: OpOpaque, Via: via})
	}
}

func (p *parser) segments(raws []string, literal bool) []Segment {
	var out []Segment
	for _, s := range raws {
		if s == "" {
			continue
		}
		if literal {
			out = append(out, Segment{Raw: s, Expanded: filepath.Clean(s)})
		} else {
			out = append(out, expandSegment(s, p.env))
		}
	}
	return out
}

// mentionsPath detects PATH manipulation this parser did not understand:
// a word-boundary "PATH=" (so MANPATH/GOPATH do not count) or a zsh
// path-array opener.
func mentionsPath(text string) bool {
	for i := 0; ; {
		j := strings.Index(text[i:], "PATH=")
		if j < 0 {
			break
		}
		j += i
		if j == 0 || !isNameByte(text[j-1], true) {
			return true
		}
		i = j + len("PATH=")
	}
	return strings.Contains(text, "path=(") || strings.Contains(text, "path+=(")
}

// recognized is the normalized result of one recognizer.
type recognized struct {
	op            Op
	before, after []string
	literal       bool // value was fully single-quoted: no expansion
	tool          string
}

// recognize tries the POSIX, zsh-array, and fish recognizers in order.
func recognize(cmd string) (recognized, bool) {
	if r, ok := parseAssign(cmd); ok {
		return r, true
	}
	if r, ok := parseZshArray(cmd); ok {
		return r, true
	}
	if r, ok := parseFishSet(cmd); ok {
		return r, true
	}
	if r, ok := parseFishAddPath(cmd); ok {
		return r, true
	}
	return recognized{}, false
}

// stripAssignPrefix removes an export/typeset/declare prefix (plus its
// option flags) so the assignment itself can be inspected.
func stripAssignPrefix(cmd string) string {
	for _, pre := range []string{"export", "typeset", "declare"} {
		if !strings.HasPrefix(cmd, pre) {
			continue
		}
		rest := cmd[len(pre):]
		if rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
			continue
		}
		rest = strings.TrimLeft(rest, " \t")
		for strings.HasPrefix(rest, "-") {
			sp := strings.IndexAny(rest, " \t")
			if sp < 0 {
				return cmd
			}
			rest = strings.TrimLeft(rest[sp:], " \t")
		}
		return rest
	}
	return cmd
}

// parseAssign handles PATH=… in all its POSIX spellings.
func parseAssign(cmd string) (recognized, bool) {
	rest := stripAssignPrefix(cmd)
	if !strings.HasPrefix(rest, "PATH=") {
		return recognized{}, false
	}
	rhs := rest[len("PATH="):]
	if strings.Contains(rhs, "$(") || strings.Contains(rhs, "`") {
		return recognized{op: OpOpaque}, true // command substitution
	}
	value, literal := cutValue(rhs)
	segs := strings.Split(value, ":")
	refIdx := -1
	for i, s := range segs {
		if literal {
			continue
		}
		if s == "$PATH" || s == "${PATH}" {
			if refIdx >= 0 {
				return recognized{op: OpOpaque}, true // multiple $PATH refs
			}
			refIdx = i
			continue
		}
		if strings.Contains(s, "$PATH") || strings.Contains(s, "${PATH") {
			return recognized{op: OpOpaque}, true // ${PATH:+…} and friends
		}
	}
	return splitAroundRef(segs, refIdx, literal), true
}

// splitAroundRef converts a segment list plus the position of the $PATH
// self-reference into a normalized recognized value.
func splitAroundRef(segs []string, refIdx int, literal bool) recognized {
	if refIdx < 0 {
		return recognized{op: OpReplace, before: segs, literal: literal}
	}
	r := recognized{before: segs[:refIdx], after: segs[refIdx+1:], literal: literal}
	switch {
	case len(r.before) > 0 && len(r.after) > 0:
		r.op = OpMixed
	case len(r.before) > 0:
		r.op = OpPrepend
	default:
		r.op = OpAppend
	}
	return r
}

// cutValue extracts an assignment value: everything up to the first
// unquoted whitespace, with quote characters removed. literal reports
// that the entire value sat inside single quotes, where shells expand
// nothing.
func cutValue(rhs string) (string, bool) {
	var b strings.Builder
	inS, inD := false, false
	sawPlain := false
loop:
	for i := 0; i < len(rhs); i++ {
		c := rhs[i]
		switch {
		case c == '\'' && !inD:
			inS = !inS
		case c == '"' && !inS:
			inD = !inD
		case (c == ' ' || c == '\t') && !inS && !inD:
			break loop
		case c == '\\' && !inS && i+1 < len(rhs):
			i++
			b.WriteByte(rhs[i])
			sawPlain = true
		default:
			b.WriteByte(c)
			if !inS {
				sawPlain = true
			}
		}
	}
	return b.String(), b.Len() > 0 && !sawPlain
}

// parseZshArray handles zsh's path array: path=(/a $path) and path+=(/b).
func parseZshArray(cmd string) (recognized, bool) {
	rest := stripAssignPrefix(cmd)
	var body string
	appendOp := false
	switch {
	case strings.HasPrefix(rest, "path+=("):
		appendOp = true
		body = rest[len("path+=("):]
	case strings.HasPrefix(rest, "path=("):
		body = rest[len("path=("):]
	default:
		return recognized{}, false
	}
	end := strings.LastIndex(body, ")")
	if end < 0 {
		return recognized{op: OpOpaque}, true // spans lines we did not join
	}
	if strings.Contains(body[:end], "$(") || strings.Contains(body[:end], "`") {
		return recognized{op: OpOpaque}, true // command substitution
	}
	fields := strings.Fields(body[:end])
	var segs []string
	refIdx := -1
	for _, f := range fields {
		f = trimQuotes(f)
		if f == "$path" || f == "$PATH" || f == "${path}" || f == "${PATH}" {
			refIdx = len(segs)
			segs = append(segs, "") // placeholder, skipped by segments()
			continue
		}
		segs = append(segs, f)
	}
	if appendOp {
		return recognized{op: OpAppend, after: segs}, true
	}
	if refIdx < 0 {
		return recognized{op: OpReplace, before: segs}, true
	}
	return splitAroundRef(segs, refIdx, false), true
}

// parseFishSet handles fish: set -gx PATH /a $PATH, and fish_user_paths
// (whose contents fish always places ahead of PATH).
func parseFishSet(cmd string) (recognized, bool) {
	fields := strings.Fields(cmd)
	if len(fields) < 3 || fields[0] != "set" {
		return recognized{}, false
	}
	i := 1
	for i < len(fields) && strings.HasPrefix(fields[i], "-") {
		i++
	}
	if i >= len(fields)-1 {
		return recognized{}, false
	}
	name := fields[i]
	if name != "PATH" && name != "fish_user_paths" {
		return recognized{}, false
	}
	if strings.Contains(cmd, "$(") || strings.Contains(cmd, "(") {
		return recognized{op: OpOpaque}, true // fish command substitution
	}
	ref := "$" + name
	var segs []string
	refIdx := -1
	for _, f := range fields[i+1:] {
		f = trimQuotes(f)
		if f == ref || f == "${"+name+"}" {
			refIdx = len(segs)
			segs = append(segs, "")
			continue
		}
		segs = append(segs, f)
	}
	if name == "fish_user_paths" {
		// Everything in fish_user_paths sits ahead of PATH regardless of
		// where the self-reference appeared.
		var kept []string
		for j, s := range segs {
			if j != refIdx {
				kept = append(kept, s)
			}
		}
		return recognized{op: OpPrepend, before: kept}, true
	}
	if refIdx < 0 {
		return recognized{op: OpReplace, before: segs}, true
	}
	return splitAroundRef(segs, refIdx, false), true
}

// parseFishAddPath handles fish_add_path, which prepends by default and
// appends with -a/--append.
func parseFishAddPath(cmd string) (recognized, bool) {
	fields := strings.Fields(cmd)
	if len(fields) < 2 || fields[0] != "fish_add_path" {
		return recognized{}, false
	}
	appendOp := false
	var segs []string
	for _, f := range fields[1:] {
		switch {
		case f == "-a" || f == "--append":
			appendOp = true
		case strings.HasPrefix(f, "-"):
			// ordering/scope flags (-g, -U, -m, -P…) do not change dirs
		default:
			segs = append(segs, trimQuotes(f))
		}
	}
	if len(segs) == 0 {
		return recognized{}, false
	}
	if appendOp {
		return recognized{op: OpAppend, after: segs, tool: "fish_add_path"}, true
	}
	return recognized{op: OpPrepend, before: segs, tool: "fish_add_path"}, true
}

// followSources recurses into files pulled in with source or "." so a
// PATH line in ~/.config/aliases.sh is still attributed precisely.
func (p *parser) followSources(file string, num int, text string, via []string) {
	if len(via) >= maxSourceDepth {
		return
	}
	for _, cmd := range splitCommands(text) {
		fields := strings.Fields(stripKeywords(cmd))
		if len(fields) < 2 {
			continue
		}
		if f0 := fields[0]; f0 != "source" && f0 != "." && f0 != `\.` {
			continue
		}
		seg := expandSegment(trimQuotes(fields[1]), p.env)
		if seg.Unresolved || seg.Expanded == "" || seg.Glob {
			continue
		}
		target := seg.Expanded
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(file), target)
		}
		chain := append(append([]string{}, via...), fmt.Sprintf("%s:%d", file, num))
		p.parseFile(target, chain)
	}
}

// splitCommands divides a line into simple commands on unquoted ";",
// "&&", and "||" so assignments buried mid-line are still found.
func splitCommands(text string) []string {
	var out []string
	var b strings.Builder
	inS, inD := false, false
	flush := func() {
		if s := strings.TrimSpace(b.String()); s != "" {
			out = append(out, s)
		}
		b.Reset()
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c == '\'' && !inD:
			inS = !inS
			b.WriteByte(c)
		case c == '"' && !inS:
			inD = !inD
			b.WriteByte(c)
		case !inS && !inD && c == ';':
			flush()
		case !inS && !inD && c == '&' && i+1 < len(text) && text[i+1] == '&':
			flush()
			i++
		case !inS && !inD && c == '|' && i+1 < len(text) && text[i+1] == '|':
			flush()
			i++
		default:
			b.WriteByte(c)
		}
	}
	flush()
	return out
}

var leadingKeywords = map[string]bool{
	"if": true, "then": true, "else": true, "elif": true, "fi": true,
	"do": true, "done": true, "while": true, "until": true,
	"{": true, "}": true, "!": true,
}

// stripKeywords drops leading shell keywords so `then PATH=…` and
// `if … ; then PATH=…` still expose the assignment.
func stripKeywords(cmd string) string {
	for {
		cmd = strings.TrimSpace(cmd)
		sp := strings.IndexAny(cmd, " \t")
		if sp < 0 {
			return cmd
		}
		if !leadingKeywords[cmd[:sp]] {
			return cmd
		}
		cmd = cmd[sp+1:]
	}
}

// trimQuotes strips one pair of matching outer quotes from a token.
func trimQuotes(s string) string {
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	return s
}

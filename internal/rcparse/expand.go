package rcparse

import (
	"path/filepath"
	"strings"
)

// Env carries the values used to expand rc-file text into concrete
// paths. Tests inject a fabricated Env; the CLI fills it from the real
// process environment plus the --home override.
type Env struct {
	Home string
	Vars map[string]string
}

func (e Env) lookup(name string) (string, bool) {
	if name == "HOME" {
		if e.Home != "" {
			return e.Home, true
		}
		return "", false
	}
	v, ok := e.Vars[name]
	if ok && v != "" {
		return v, true
	}
	return "", false
}

// Segment is one colon- or space-separated element of a PATH mutation.
type Segment struct {
	Raw        string // as written in the rc file, e.g. "$HOME/.cargo/bin"
	Expanded   string // after ~/$VAR expansion; partially expanded if Unresolved
	Unresolved bool   // an unknown variable survived expansion
	Glob       bool   // Expanded contains * or ? (tool-hook patterns)
}

// expandSegment resolves ~ and $VAR / ${VAR} references in one raw
// segment. Unknown variables are left in place and flagged, so
// provenance matching never guesses.
func expandSegment(raw string, env Env) Segment {
	s := Segment{Raw: raw}
	text := raw
	switch {
	case text == "~":
		if env.Home != "" {
			text = env.Home
		} else {
			s.Unresolved = true
		}
	case strings.HasPrefix(text, "~/"):
		if env.Home != "" {
			text = env.Home + text[1:]
		} else {
			s.Unresolved = true
		}
	}
	var b strings.Builder
	for i := 0; i < len(text); {
		c := text[i]
		if c != '$' {
			b.WriteByte(c)
			i++
			continue
		}
		name, next := varName(text, i+1)
		if name == "" {
			b.WriteByte(c)
			i++
			continue
		}
		if v, ok := env.lookup(name); ok {
			b.WriteString(v)
		} else {
			b.WriteString(text[i:next])
			s.Unresolved = true
		}
		i = next
	}
	out := b.String()
	s.Glob = strings.ContainsAny(out, "*?")
	if !s.Unresolved && out != "" {
		out = filepath.Clean(out)
	}
	s.Expanded = out
	return s
}

// varName parses $NAME or ${NAME} starting at text[i] (just after the
// '$'). It returns the variable name and the index one past the
// reference, or "" when the '$' does not start a valid reference.
func varName(text string, i int) (string, int) {
	if i < len(text) && text[i] == '{' {
		end := strings.IndexByte(text[i:], '}')
		if end < 0 {
			return "", i
		}
		name := text[i+1 : i+end]
		if !validName(name) {
			return "", i
		}
		return name, i + end + 1
	}
	j := i
	for j < len(text) && isNameByte(text[j], j > i) {
		j++
	}
	if j == i {
		return "", i
	}
	return text[i:j], j
}

func validName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isNameByte(s[i], i > 0) {
			return false
		}
	}
	return true
}

func isNameByte(c byte, notFirst bool) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		return true
	case c >= '0' && c <= '9':
		return notFirst
	}
	return false
}

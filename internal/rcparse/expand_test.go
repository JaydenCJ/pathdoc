// Tests for segment expansion: ~, $VAR, ${VAR}, unknown variables, and
// glob detection. Expansion must never guess — unknown variables stay in
// the text and mark the segment unresolved.
package rcparse

import "testing"

var testEnv = Env{Home: "/home/dev", Vars: map[string]string{"NVM_DIR": "/home/dev/.nvm"}}

func TestExpandTildeForms(t *testing.T) {
	cases := []struct {
		raw, want string
	}{
		{"~/bin", "/home/dev/bin"},
		{"~", "/home/dev"},
	}
	for _, c := range cases {
		if s := expandSegment(c.raw, testEnv); s.Expanded != c.want || s.Unresolved {
			t.Fatalf("%q → %+v, want %q", c.raw, s, c.want)
		}
	}
	// Without a home there is nothing to expand against — must not guess.
	if s := expandSegment("~/bin", Env{}); !s.Unresolved {
		t.Fatalf("no home available — must be unresolved, got %+v", s)
	}
}

func TestExpandVariables(t *testing.T) {
	cases := []struct {
		raw, want string
	}{
		{"$HOME/.local/bin", "/home/dev/.local/bin"},
		{"${HOME}/go/bin", "/home/dev/go/bin"},
		{"$NVM_DIR/versions/node/v20.1.0/bin", "/home/dev/.nvm/versions/node/v20.1.0/bin"},
		{"/opt/a$", "/opt/a$"}, // a trailing $ is a literal character
	}
	for _, c := range cases {
		if s := expandSegment(c.raw, testEnv); s.Expanded != c.want || s.Unresolved {
			t.Fatalf("%q → %+v, want %q", c.raw, s, c.want)
		}
	}
	s := expandSegment("$MYSTERY/bin", testEnv)
	if !s.Unresolved {
		t.Fatalf("unknown var must mark segment unresolved: %+v", s)
	}
	// The variable text is preserved so the renderer can show it.
	if s.Expanded != "$MYSTERY/bin" {
		t.Fatalf("unresolved text mangled: %q", s.Expanded)
	}
}

func TestExpandGlobDetectionAndCleaning(t *testing.T) {
	if s := expandSegment("~/.nvm/versions/node/*/bin", testEnv); !s.Glob {
		t.Fatalf("glob not detected: %+v", s)
	}
	if s := expandSegment("$HOME//bin/", testEnv); s.Expanded != "/home/dev/bin" {
		t.Fatalf("expected cleaned path, got %q", s.Expanded)
	}
}

package rcparse

import "strings"

// Version managers rarely write a literal PATH= line into rc files —
// they hide the mutation behind an eval or a generated script. Each
// hookDef recognizes such an invocation and records the directories the
// tool is known to add, as expandable patterns. Attribution through a
// hook is a strong hint, not a proof, which is why the mutation carries
// Op == OpHook and the tool name for the renderer to say so.
type hookDef struct {
	triggers []string // any of these substrings fires the hook
	tool     string
	patterns []string // candidate segments; ~ and $VAR expanded, * allowed
}

var hookTable = []hookDef{
	{[]string{"pyenv init"}, "pyenv", []string{"$PYENV_ROOT/shims", "~/.pyenv/shims"}},
	{[]string{"rbenv init"}, "rbenv", []string{"$RBENV_ROOT/shims", "~/.rbenv/shims"}},
	{[]string{"nodenv init"}, "nodenv", []string{"~/.nodenv/shims"}},
	{[]string{"goenv init"}, "goenv", []string{"~/.goenv/shims"}},
	{[]string{"jenv init"}, "jenv", []string{"~/.jenv/shims"}},
	{[]string{"nvm.sh"}, "nvm", []string{"$NVM_DIR/versions/node/*/bin", "~/.nvm/versions/node/*/bin"}},
	{[]string{"cargo/env"}, "rustup", []string{"$CARGO_HOME/bin", "~/.cargo/bin"}},
	{[]string{"brew shellenv"}, "homebrew", []string{
		"/opt/homebrew/bin", "/opt/homebrew/sbin",
		"/usr/local/bin", "/usr/local/sbin",
		"/home/linuxbrew/.linuxbrew/bin", "/home/linuxbrew/.linuxbrew/sbin"}},
	{[]string{"sdkman-init.sh"}, "sdkman", []string{"~/.sdkman/candidates/*/current/bin"}},
	{[]string{"conda.sh", "conda shell.", "conda' 'shell."}, "conda", []string{
		"~/miniconda3/bin", "~/miniconda3/condabin",
		"~/anaconda3/bin", "~/anaconda3/condabin",
		"/opt/conda/bin", "/opt/conda/condabin"}},
	{[]string{"asdf.sh"}, "asdf", []string{"$ASDF_DATA_DIR/shims", "~/.asdf/shims"}},
	{[]string{"mise activate"}, "mise", []string{"~/.local/share/mise/shims"}},
	{[]string{"volta setup", "VOLTA_HOME"}, "volta", []string{"$VOLTA_HOME/bin", "~/.volta/bin"}},
	{[]string{"fnm env"}, "fnm", []string{"~/.local/share/fnm", "~/.fnm"}},
}

// scanHooks emits one hook mutation per matching tool on this line.
func (p *parser) scanHooks(file string, num int, text string, via []string) {
	for _, h := range hookTable {
		if !anyContains(text, h.triggers) {
			continue
		}
		m := Mutation{File: file, Line: num, Text: text, Op: OpHook, Tool: h.tool, Via: via}
		for _, pat := range h.patterns {
			m.Prepends = append(m.Prepends, expandSegment(pat, p.env))
		}
		p.muts = append(p.muts, m)
	}
}

func anyContains(text string, subs []string) bool {
	for _, s := range subs {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

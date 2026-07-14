package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/JaydenCJ/pathdoc/internal/pathenv"
	"github.com/JaydenCJ/pathdoc/internal/provenance"
	"github.com/JaydenCJ/pathdoc/internal/rcparse"
	"github.com/JaydenCJ/pathdoc/internal/report"
	"github.com/JaydenCJ/pathdoc/internal/scan"
	"github.com/JaydenCJ/pathdoc/internal/shadow"
)

// model runs the full analysis pipeline with the resolved flag values:
// classify → scan → shadow, plus rc scanning unless disabled. Every
// input has an explicit override so tests never depend on the host.
func (c *common) model() *report.Model {
	pathStr := c.pathStr
	if !c.pathSet && pathStr == "" {
		pathStr = os.Getenv("PATH")
	}
	home := c.home
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		}
	}
	entries := pathenv.Classify(pathStr)
	idx := scan.Build(entries)
	m := &report.Model{
		Path:      pathStr,
		Entries:   entries,
		Index:     idx,
		Conflicts: shadow.Analyze(idx),
		Home:      home,
	}
	if c.noProv {
		return m
	}
	files := make([]string, 0, len(c.rc))
	for _, f := range c.rc {
		if strings.HasPrefix(f, "~/") && home != "" {
			f = filepath.Join(home, f[2:])
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		shell := c.shell
		if shell == "" {
			if s := os.Getenv("SHELL"); s != "" {
				shell = filepath.Base(s)
			} else {
				shell = "bash"
			}
		}
		files = rcparse.DefaultRCFiles(shell, home)
	}
	muts, read := rcparse.Scan(files, rcparse.Env{Home: home, Vars: envVars()})
	m.Prov = provenance.Attribute(entries, muts, read)
	return m
}

// envVars snapshots the process environment for $VAR expansion in rc
// lines (e.g. $NVM_DIR, $PYENV_ROOT set by earlier startup files).
func envVars() map[string]string {
	vars := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			vars[k] = v
		}
	}
	return vars
}

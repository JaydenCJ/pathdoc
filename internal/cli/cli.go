// Package cli implements the pathdoc command-line interface. Run takes
// argv and two writers and returns an exit code, so every subcommand is
// testable in-process without building a binary.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/JaydenCJ/pathdoc/internal/version"
)

// Exit codes. Documented in the README; `check` and `which` use
// ExitFindings as their machine-readable verdict.
const (
	ExitOK       = 0
	ExitFindings = 1 // check findings at/above the threshold, or name not found
	ExitUsage    = 2
	ExitRuntime  = 3
)

// Run dispatches argv and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return runReport(nil, stdout, stderr)
	}
	switch args[0] {
	case "report":
		return runReport(args[1:], stdout, stderr)
	case "which":
		return runWhich(args[1:], stdout, stderr)
	case "shadows":
		return runShadows(args[1:], stdout, stderr)
	case "rc":
		return runRC(args[1:], stdout, stderr)
	case "dedupe":
		return runDedupe(args[1:], stdout, stderr)
	case "check":
		return runCheck(args[1:], stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "pathdoc %s\n", version.Version)
		return ExitOK
	case "help", "--help", "-h":
		usage(stdout)
		return ExitOK
	default:
		if strings.HasPrefix(args[0], "-") {
			// Bare flags: treat as `report <flags>`.
			return runReport(args, stdout, stderr)
		}
		fmt.Fprintf(stderr, "pathdoc: unknown subcommand %q\n\n", args[0])
		usage(stderr)
		return ExitUsage
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `pathdoc — diagnose $PATH: duplicates, dead directories, shadowed
binaries, which entry wins, and which rc line added each entry.

usage:
  pathdoc [report] [flags]       full diagnosis: entries, issues, shadowing
  pathdoc which <command>...     every PATH candidate for a command, winner first
  pathdoc shadows [--all]        shadowing conflicts (--all includes benign ones)
  pathdoc rc                     every PATH-modifying rc line, in startup order
  pathdoc dedupe [flags]         emit a cleaned PATH (duplicates removed)
  pathdoc check [--fail-on L]    exit 1 on findings at/above L (warn|error)
  pathdoc version                print the version

flags (every subcommand):
  --path string    diagnose this PATH value instead of $PATH
  --home string    home directory for ~ expansion (default $HOME)
  --shell string   shell whose startup files to scan: bash, zsh, fish, sh
  --rc file        rc file to scan for provenance (repeatable; replaces defaults)
  --no-provenance  skip rc-file scanning entirely
  --format string  output format: text or json (default text)

exit codes: 0 ok · 1 findings / not found · 2 usage error · 3 runtime error
`)
}

// multiFlag is a repeatable string flag.
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// common holds the flags shared by every subcommand.
type common struct {
	pathStr string
	pathSet bool // --path was passed explicitly, even if empty
	home    string
	shell   string
	format  string
	rc      multiFlag
	noProv  bool
}

func (c *common) register(fs *flag.FlagSet) {
	fs.StringVar(&c.pathStr, "path", "", "diagnose this PATH value instead of $PATH")
	fs.StringVar(&c.home, "home", "", "home directory for ~ expansion (default $HOME)")
	fs.StringVar(&c.shell, "shell", "", "shell whose startup files to scan: bash, zsh, fish, sh")
	fs.Var(&c.rc, "rc", "rc file to scan for provenance (repeatable; replaces the default set)")
	fs.BoolVar(&c.noProv, "no-provenance", false, "skip rc-file scanning")
	fs.StringVar(&c.format, "format", "text", "output format: text or json")
}

// parseArgs builds and parses a FlagSet for one subcommand, validating
// the shared flags. It returns a nil FlagSet when parsing stopped; the
// exit code then distinguishes an explicit help request (ExitOK) from a
// genuine usage error (ExitUsage).
func parseArgs(name string, args []string, stderr io.Writer, c *common, extra func(*flag.FlagSet)) (*flag.FlagSet, int) {
	fs := flag.NewFlagSet("pathdoc "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	c.register(fs)
	if extra != nil {
		extra(fs)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, ExitOK // asking for help is not a usage error
		}
		return nil, ExitUsage
	}
	// `--path ""` must diagnose an explicitly empty PATH (a single empty
	// segment, i.e. "current directory only") — not silently fall back to
	// the live $PATH, which would misreport the very hazard being probed.
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "path" {
			c.pathSet = true
		}
	})
	if c.format != "text" && c.format != "json" {
		fmt.Fprintf(stderr, "pathdoc %s: unsupported --format %q (want text or json)\n", name, c.format)
		return nil, ExitUsage
	}
	return fs, ExitOK
}

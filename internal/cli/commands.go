package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/JaydenCJ/pathdoc/internal/pathenv"
	"github.com/JaydenCJ/pathdoc/internal/report"
)

func runReport(args []string, stdout, stderr io.Writer) int {
	var c common
	fs, code := parseArgs("report", args, stderr, &c, nil)
	if fs == nil {
		return code
	}
	m := c.model()
	if c.format == "json" {
		if err := report.JSON(stdout, m); err != nil {
			fmt.Fprintf(stderr, "pathdoc report: %v\n", err)
			return ExitRuntime
		}
		return ExitOK
	}
	report.Text(stdout, m)
	return ExitOK
}

func runWhich(args []string, stdout, stderr io.Writer) int {
	var c common
	fs, code := parseArgs("which", args, stderr, &c, nil)
	if fs == nil {
		return code
	}
	names := fs.Args()
	if len(names) == 0 {
		fmt.Fprintln(stderr, "pathdoc which: at least one command name is required")
		return ExitUsage
	}
	m := c.model()
	missing := 0
	for _, name := range names {
		if len(m.Index.Lookup(name)) == 0 {
			missing++
		}
	}
	if c.format == "json" {
		if err := report.WhichJSON(stdout, m, names); err != nil {
			fmt.Fprintf(stderr, "pathdoc which: %v\n", err)
			return ExitRuntime
		}
	} else {
		report.WhichText(stdout, m, names)
	}
	if missing > 0 {
		return ExitFindings
	}
	return ExitOK
}

func runShadows(args []string, stdout, stderr io.Writer) int {
	var c common
	var all bool
	fs, code := parseArgs("shadows", args, stderr, &c, func(fs *flag.FlagSet) {
		fs.BoolVar(&all, "all", false, "include benign conflicts (same underlying file)")
	})
	if fs == nil {
		return code
	}
	m := c.model()
	if c.format == "json" {
		if err := report.ShadowsJSON(stdout, m, all); err != nil {
			fmt.Fprintf(stderr, "pathdoc shadows: %v\n", err)
			return ExitRuntime
		}
		return ExitOK
	}
	report.ShadowsText(stdout, m, all)
	return ExitOK
}

func runRC(args []string, stdout, stderr io.Writer) int {
	var c common
	fs, code := parseArgs("rc", args, stderr, &c, nil)
	if fs == nil {
		return code
	}
	m := c.model()
	if c.format == "json" {
		if err := report.RCJSON(stdout, m); err != nil {
			fmt.Fprintf(stderr, "pathdoc rc: %v\n", err)
			return ExitRuntime
		}
		return ExitOK
	}
	report.RCText(stdout, m)
	return ExitOK
}

func runDedupe(args []string, stdout, stderr io.Writer) int {
	var c common
	var dropDead, dropUnsafe bool
	var emit string
	fs, code := parseArgs("dedupe", args, stderr, &c, func(fs *flag.FlagSet) {
		fs.BoolVar(&dropDead, "drop-dead", false, "also remove entries that do not exist or are not directories")
		fs.BoolVar(&dropUnsafe, "drop-unsafe", false, "also remove empty, relative, unexpanded-~ and world-writable entries")
		fs.StringVar(&emit, "emit", "plain", "output form: plain, export, or fish")
	})
	if fs == nil {
		return code
	}
	if emit != "plain" && emit != "export" && emit != "fish" {
		fmt.Fprintf(stderr, "pathdoc dedupe: unsupported --emit %q (want plain, export, or fish)\n", emit)
		return ExitUsage
	}
	c.noProv = true // dedupe never needs rc scanning
	m := c.model()
	var kept []string
	removed := 0
	for _, e := range m.Entries {
		if dropEntry(e, dropDead, dropUnsafe) {
			removed++
			continue
		}
		kept = append(kept, e.Raw)
	}
	switch emit {
	case "export":
		fmt.Fprintf(stdout, "export PATH=\"%s\"\n", strings.Join(kept, ":"))
	case "fish":
		quoted := make([]string, 0, len(kept))
		for _, k := range kept {
			quoted = append(quoted, fishQuote(k))
		}
		fmt.Fprintf(stdout, "set -gx PATH %s\n", strings.Join(quoted, " "))
	default:
		fmt.Fprintln(stdout, strings.Join(kept, ":"))
	}
	fmt.Fprintf(stderr, "pathdoc dedupe: kept %d of %d entries (%d removed)\n",
		len(kept), len(m.Entries), removed)
	return ExitOK
}

// dropEntry decides whether dedupe removes an entry. Duplicates always
// go; the rest is opt-in so the emitted PATH is never surprising.
func dropEntry(e pathenv.Entry, dropDead, dropUnsafe bool) bool {
	for _, k := range e.Issues {
		switch k {
		case pathenv.IssueDuplicate, pathenv.IssueSymlinkDuplicate:
			return true
		case pathenv.IssueDead, pathenv.IssueNotDir:
			if dropDead {
				return true
			}
		case pathenv.IssueEmpty, pathenv.IssueRelative, pathenv.IssueTilde, pathenv.IssueWorldWritable:
			if dropUnsafe {
				return true
			}
		}
	}
	return false
}

func fishQuote(s string) string {
	if s == "" || strings.ContainsAny(s, " \t'\"") {
		return "\"" + strings.ReplaceAll(s, "\"", "\\\"") + "\""
	}
	return s
}

func runCheck(args []string, stdout, stderr io.Writer) int {
	var c common
	var failOn string
	fs, code := parseArgs("check", args, stderr, &c, func(fs *flag.FlagSet) {
		fs.StringVar(&failOn, "fail-on", "warn", "fail when findings at/above this severity exist: warn or error")
	})
	if fs == nil {
		return code
	}
	var level pathenv.Severity
	switch failOn {
	case "warn":
		level = pathenv.Warn
	case "error":
		level = pathenv.Error
	default:
		fmt.Fprintf(stderr, "pathdoc check: unsupported --fail-on %q (want warn or error)\n", failOn)
		return ExitUsage
	}
	m := c.model()
	findings := report.Findings(m)
	failures := 0
	fmt.Fprintf(stdout, "pathdoc check — fail on %s and above\n\n", failOn)
	for _, f := range findings {
		fmt.Fprintf(stdout, "  [%-5s] %s\n", f.Severity, f.Text)
		if f.Severity >= level {
			failures++
		}
	}
	if len(findings) > 0 {
		fmt.Fprintln(stdout)
	}
	if failures > 0 {
		fmt.Fprintf(stdout, "check: FAIL (%d finding(s) at or above %s)\n", failures, failOn)
		return ExitFindings
	}
	fmt.Fprintf(stdout, "check: ok (no findings at or above %s)\n", failOn)
	return ExitOK
}

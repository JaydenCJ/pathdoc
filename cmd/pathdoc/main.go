// Command pathdoc diagnoses $PATH: duplicates, dead directories,
// shadowed binaries, which entry wins — and which rc line added each
// entry in the first place.
package main

import (
	"os"

	"github.com/JaydenCJ/pathdoc/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}

// Command heavyq is a priority, resource-aware queue for heavy commands.
package main

import (
	"os"

	"github.com/LysCongThanh/heavyq/internal/cli"
)

func main() {
	if cli.LifelineRequested() { // started by heavyq to run a local command (see internal/cli/lifeline_process.go)
		os.Exit(cli.RunLifeline(os.Args[1:]))
	}
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}

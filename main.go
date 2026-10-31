// Command heavyq is a priority, resource-aware queue for heavy commands.
package main

import (
	"os"

	"github.com/LysCongThanh/heavyq/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}

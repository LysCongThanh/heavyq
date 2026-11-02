package cli

import (
	"fmt"
	"io"
)

// completionCommand implements `heavyq completion bash|zsh|fish`: the script
// for the shell on stdout. It reads no config and touches no files.
func completionCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: heavyq completion bash|zsh|fish")
		return ExitUsage
	}
	switch args[0] {
	case "bash":
		writeBashCompletion(stdout)
	case "zsh":
		writeZshCompletion(stdout)
	case "fish":
		writeFishCompletion(stdout)
	default:
		fmt.Fprintf(stderr, "heavyq: no completion for shell %q (bash, zsh, fish)\n", args[0])
		return ExitUsage
	}
	return 0
}

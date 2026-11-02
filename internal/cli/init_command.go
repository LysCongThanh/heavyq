package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
)

const initUsage = `usage: heavyq init [--agents-md] [--force] [--print]

Writes heavyq.toml at the git root with the gate heavyq detects from go.mod, package.json,
pytest config and Cargo.toml (nothing is run to detect it). It never overwrites an existing
heavyq.toml (exit 1) unless --force. Safe to run from an agent.

  --print      print the file to stdout; write nothing (not with --agents-md)
  --agents-md  also add a short "run heavyq gate run ready before saying done" section to
               AGENTS.md (created if missing) and CLAUDE.md (only if it exists), between
               <!-- heavyq:begin --> and <!-- heavyq:end -->. Running it again changes nothing.
               Applied even when heavyq.toml already exists.
  --force      overwrite an existing heavyq.toml
`

// initCommand implements `heavyq init [--agents-md] [--force] [--print]`: it
// writes down, as heavyq.toml, the default gate that gate status and gate run
// would otherwise detect on the fly. Exit 0 done, 1 when heavyq.toml exists
// (and --force is absent) or a file could not be written, 2 on a usage error
// or outside a git work tree.
func initCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	agentsMD := flags.Bool("agents-md", false, "add the heavyq section to AGENTS.md (and CLAUDE.md if present)")
	force := flags.Bool("force", false, "overwrite an existing heavyq.toml")
	printOnly := flags.Bool("print", false, "print heavyq.toml to stdout and write nothing")
	flags.SetOutput(stderr)
	flags.Usage = func() { fmt.Fprint(stderr, initUsage) }
	if err := flags.Parse(args); err != nil {
		return ExitUsage
	}
	if flags.NArg() != 0 {
		flags.Usage()
		return ExitUsage
	}
	if *printOnly && *agentsMD {
		fmt.Fprintln(stderr, "heavyq: --print shows heavyq.toml only; use it without --agents-md")
		return ExitUsage
	}
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	root, err := gitutil.TopLevel(wd)
	if err != nil {
		fmt.Fprintf(stderr, "heavyq: %s is not inside a git work tree; heavyq.toml belongs at the git root\n", wd)
		return ExitUsage
	}
	detected := repoconfig.Detect(root)
	text := repoconfig.RenderFile(detected)
	if *printOnly {
		fmt.Fprint(stdout, text)
		return 0
	}

	rc := 0
	path := filepath.Join(root, repoconfig.FileName)
	switch err := writeRepoConfig(path, text, *force); {
	case errors.Is(err, fs.ErrExist):
		fmt.Fprintf(stderr, "heavyq: %s already exists, left as it is (--force overwrites it, --print shows what would be written)\n", path)
		rc = 1
	case err != nil:
		fmt.Fprintln(stderr, "heavyq:", err)
		rc = 1
	case detected == nil:
		fmt.Fprintf(stderr, "heavyq: wrote %s, but found no go.mod, package.json, pytest config or Cargo.toml: it defines no gate yet, uncomment the example in it\n", path)
	default:
		g := detected.Gates[0]
		fmt.Fprintf(stdout, "wrote %s\n  gate %s, detected from %s: %s\n", path, g.Name, strings.Join(g.Sources, ", "), strings.Join(g.Run, ", "))
		if hint := nestedWorkTreesHint(root); hint != "" {
			fmt.Fprintln(stdout, hint)
		}
		fmt.Fprintf(stdout, "next: heavyq gate run %s\n", g.Name)
	}
	if *agentsMD && !updateAgentDocs(root, stdout, stderr) {
		rc = 1
	}
	return rc
}

// writeRepoConfig writes text to path. Without force an existing file (or a
// symlink, even a dangling one) is never touched: the error is then
// fs.ErrExist. With force it is replaced.
func writeRepoConfig(path, text string, force bool) error {
	if force {
		return replaceFile(path, []byte(text), 0o644)
	}
	return writeNewFile(path, text)
}

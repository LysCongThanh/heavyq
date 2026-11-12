// Command schemagen writes the JSON Schemas of heavyq's four file formats from the Go types the code
// really uses, so the schemas cannot drift from the parsers:
//
//	heavyq.schema.json  heavyq.toml, the per-repository gates ([[gate]], [[pool]], [anchor])
//	config.schema.json  the user config, ~/.config/heavyq/config.toml
//	status.schema.json  the output of `heavyq status --json` (schema 1)
//	fleet.schema.json   the output of `heavyq fleet --json` (schema 1)
//
// It is a separate module (own go.mod) so the main module keeps its stdlib + BurntSushi/toml rule; the
// reflection library lives only here. Run it from this directory, then commit the result:
//
//	cd tools/schemagen && go run .
//
// CI regenerates the files and fails when `git diff schema/` is not empty.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	root := flag.String("root", "../..", "repository root (Go comments of internal/ become descriptions)")
	out := flag.String("out", "schema", "directory to write the schemas to, relative to -root")
	flag.Parse()

	// The comment reader keys types by the path it is given, so it must run from the repository root.
	if err := os.Chdir(*root); err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal(err)
	}
	for _, f := range schemaFiles() {
		data, err := build(f)
		if err != nil {
			fatal(fmt.Errorf("%s: %w", f.File, err))
		}
		if err := os.WriteFile(filepath.Join(*out, f.File), data, 0o644); err != nil {
			fatal(err)
		}
		fmt.Println("wrote", filepath.Join(*out, f.File))
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "schemagen:", err)
	os.Exit(1)
}

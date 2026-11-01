package stance

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
)

//go:embed builtin/*.md
var builtinFS embed.FS

// Builtins returns the stances compiled into the binary, in name order. A
// malformed built-in is a build defect, so it is an error (the tests parse
// them all), never skipped.
func Builtins() ([]Stance, error) {
	entries, err := fs.ReadDir(builtinFS, "builtin")
	if err != nil {
		return nil, err
	}
	var out []Stance
	for _, e := range entries {
		data, err := builtinFS.ReadFile("builtin/" + e.Name())
		if err != nil {
			return nil, err
		}
		// The embedded path doubles as the error prefix: "builtin/x.md:3: ...".
		s, err := Parse("builtin/"+e.Name(), data)
		if err != nil {
			return nil, fmt.Errorf("built-in stance: %w", err)
		}
		s.Source, s.Path = SourceBuiltin, "builtin:"+strings.TrimSuffix(e.Name(), ".md")
		out = append(out, s)
	}
	return out, nil
}

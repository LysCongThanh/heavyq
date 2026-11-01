package anchor

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// extraGolden is the golden shape for ParseExtra output.
type extraGolden struct {
	Items    []Item    `json:"items"`
	Warnings []Warning `json:"warnings"`
}

// TestGolden parses every testdata/*.md and compares the result with the
// matching .golden.json. extra_*.md go through ParseExtra, the rest through
// Parse. Review the diff after `go test ./internal/anchor -update`.
func TestGolden(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "*.md"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, p := range files {
		name := filepath.Base(p)
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			var v any
			if strings.HasPrefix(name, "extra_") {
				items, warns := ParseExtra("plans/"+name, data)
				v = extraGolden{items, warns}
			} else {
				v = Parse(name, data)
			}
			got, err := json.MarshalIndent(v, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			golden := strings.TrimSuffix(p, ".md") + ".golden.json"
			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run go test ./internal/anchor -update)", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("%s differs from golden:\n%s\nwant:\n%s", name, got, want)
			}
		})
	}
}

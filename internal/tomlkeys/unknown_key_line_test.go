package tomlkeys

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

type testSchema struct {
	Gate []struct {
		Name string `toml:"name"`
	} `toml:"gate"`
	Anchor struct {
		File string `toml:"file"`
	} `toml:"anchor"`
}

func TestCheck(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"valid", "[[gate]]\nname = \"A\"\n[anchor]\nfile = \"X.md\"\n", ""},
		{"empty", "", ""},
		{"unknown key with line", "[[gate]]\nname = \"a\"\nrun = 1\n", `f.toml:3: unknown key "gate.run"`},
		{"unknown table", "\n# c\n[stop]\ndone = true\n", `f.toml:3: unknown key "stop"`},
		{"upper case table", "[[GATE]]\nname = \"a\"\n", `f.toml:1: unknown key "GATE"`},
		{"upper case key", "[anchor]\nFile = \"a\"\n", `f.toml:2: unknown key "anchor.File"`},
		{"case twin", "[[gate]]\nname = \"a\"\n[[gaTe]]\nname = \"b\"\n", `f.toml:3: unknown key "gaTe"`},
		{"quoted key", "[\"Anchor\"]\nfile = \"a\"\n", `f.toml:1: unknown key "Anchor"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var s testSchema
			md, err := toml.Decode(c.body, &s)
			if err != nil {
				t.Fatal(err)
			}
			err = Check("f.toml", []byte(c.body), md)
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Fatalf("error %v, want %q", err, c.want)
			}
		})
	}
}

func TestLineOfAbsentKeyIsZero(t *testing.T) {
	if got := Line([]byte("a = 1\n"), toml.Key{"missing"}); got != 0 {
		t.Fatalf("Line of an absent key = %d, want 0", got)
	}
}

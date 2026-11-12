package repoconfig

import (
	"reflect"
	"strings"
	"testing"
)

// maxFuzzInput bounds every fuzz input so one execution stays fast; the real
// limit (maxFileBytes) is 1 MiB.
const maxFuzzInput = 64 << 10

// FuzzParse feeds arbitrary bytes to the unexported parse that Load uses for
// heavyq.toml. It must never panic, must be deterministic, must name the file
// in every error, and everything it accepts must satisfy the documented
// limits (fail closed: nothing invalid slips through as a gate).
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"",
		"[[gate]]\nname = \"ready\"\nrun = [\"gofmt -l . | (! grep .)\", \"go vet ./...\", \"go test -race ./...\"]\nprio = 2\nlight = true\nremote = \"auto\"\n",
		"[[gate]]\nname = \"smoke_1-b\"\nrun = [\"make smoke\"]\n\n[anchor]\nfile = \".heavyq/anchor.md\"\nextra = [\"plans/*/phase-*.md\", \"docs/**/*.md\"]\n",
		"[[gate]]\nname = \"a\"\nrun = [\"x\"]\n[[gate]]\nname = \"a\"\nrun = [\"y\"]\n",
		"[[gate]]\nname = \"Bad Name\"\nrun = []\n",
		"[[gate]]\nname = \"a\"\nrun = [\"x\"]\nprio = 0\n",
		"[[gate]]\nname = \"a\"\nrun = [\"x\"]\nremote = \"../evil\"\n",
		"[[gate]]\nname = \"a\"\nrun = [\"x\"]\nunknown = 1\n",
		"[anchor]\nfile = \"/etc/passwd\"\n",
		"[anchor]\nextra = [\"../x\", \"[\"]\n",
		"[[gate]]\nname = \"a\"\nrun = [\"\\u0000\"]\n",
		"name = = =\n[[",
		"\xff\xfe[[gate]]",
	} {
		f.Add([]byte(s))
	}
	const root, path = "/repo", "/repo/heavyq.toml"
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzInput {
			t.Skip("input over the fuzz bound")
		}
		repo, err := parse(root, path, data)
		again, err2 := parse(root, path, data)
		if (err == nil) != (err2 == nil) || !reflect.DeepEqual(repo, again) {
			t.Fatalf("parse is not deterministic for %q", data)
		}
		if err != nil {
			if repo != nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("error must come with no repo and name the file: repo=%v err=%v", repo, err)
			}
			return
		}
		if repo.Root != root || repo.Path != path {
			t.Fatalf("repo location lost: %+v", repo)
		}
		if f := repo.Anchor.File; f != "" && CheckAnchorFile(f) != nil {
			t.Fatalf("accepted anchor file %q", f)
		}
		seen := map[string]bool{}
		for _, g := range repo.Gates {
			if !gateName.MatchString(g.Name) || seen[g.Name] {
				t.Fatalf("accepted gate name %q (seen: %v)", g.Name, seen)
			}
			seen[g.Name] = true
			if len(g.Run) == 0 || g.Prio < MinPrio || g.Prio > MaxPrio || validRemote(g.Remote) != nil {
				t.Fatalf("accepted invalid gate %+v", g)
			}
			for _, cmd := range g.Run {
				if strings.TrimSpace(cmd) == "" || len(cmd) > MaxRunLen || strings.ContainsRune(cmd, 0) {
					t.Fatalf("accepted invalid command %q in gate %q", cmd, g.Name)
				}
			}
		}
	})
}

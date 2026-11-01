package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/proof"
)

// goModRepo is the rig's git repository (see gitRepo) turned into a tiny,
// dependency-free Go module with one passing test, committed, and with no
// heavyq.toml: the case a default gate is for. The Go build cache is the real
// one, not the rig's empty XDG cache, so the real `go vet` and `go test` that
// the detected gate runs do not rebuild the standard library.
func goModRepo(t *testing.T, h *rig) string {
	t.Helper()
	if out, err := exec.Command("go", "env", "GOCACHE").Output(); err == nil {
		h.env = append(h.env, "GOCACHE="+strings.TrimSpace(string(out)))
	}
	repo := gitRepo(t, h)
	files := map[string]string{
		"go.mod":       "module example.com/zero\n\ngo 1.21\n",
		"zero.go":      "package zero\n\nfunc One() int { return 1 }\n",
		"zero_test.go": "package zero\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {\n\tif One() != 1 {\n\t\tt.Fatal(One())\n\t}\n}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, h, repo, "add", ".")
	gitIn(t, h, repo, "commit", "-q", "-m", "go module")
	return repo
}

func TestE2EDetectedGateStatusAndExplicitFileWins(t *testing.T) {
	h := newRig(t, baseConfig)
	repo := goModRepo(t, h)

	rc, out, errOut := h.runSplit("gate", "status")
	if rc != 1 || !strings.Contains(out, "gate ready (detected): NOT PROVEN") ||
		!strings.Contains(out, "go vet ./...") || !strings.Contains(out, "go test ./...") ||
		!strings.Contains(out, "Prove it with: heavyq gate run ready\n") {
		t.Fatalf("gate status rc=%d\nstdout: %s\nstderr: %s", rc, out, errOut)
	}
	rc, g := h.gateJSON()
	if rc != 1 || g.Gate != "ready" || len(g.Commands) != 2 || g.Commands[0].Cmd != "go vet ./..." || g.Commands[1].State != proof.Missing {
		t.Fatalf("gate status --json rc=%d %+v", rc, g)
	}
	_, raw, _ := h.runSplit("gate", "status", "--json")
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil || doc["detected"] != true {
		t.Fatalf(`--json must say "detected": true: %v\n%s`, err, raw)
	}
	if rc, out, _ := h.runSplit("gate", "status", "nope"); rc != ExitUsage {
		t.Fatalf("unknown gate name: rc=%d\n%s", rc, out)
	}

	// A heavyq.toml, whatever it holds, wins over the detection.
	toml := "[[gate]]\nname = \"ready\"\nrun = [\"true\"]\n"
	if err := os.WriteFile(filepath.Join(repo, "heavyq.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	rc, out, errOut = h.runSplit("gate", "status")
	if rc != 1 || !strings.Contains(out, "gate ready: NOT PROVEN") || strings.Contains(out, "detected") || strings.Contains(out, "go vet") {
		t.Fatalf("explicit file: rc=%d\nstdout: %s\nstderr: %s", rc, out, errOut)
	}
	_, raw, _ = h.runSplit("gate", "status", "--json")
	doc = nil
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	if _, has := doc["detected"]; has {
		t.Fatalf("a gate from a file must not carry the detected key:\n%s", raw)
	}
}

func TestE2EDetectedGateRunProvesAndWhyReportsIt(t *testing.T) {
	h := newRig(t, baseConfig)
	goModRepo(t, h)

	rc, out, errOut := h.runSplit("gate", "run")
	if rc != 0 || !strings.Contains(errOut, "default gate detected from go.mod") || !strings.Contains(out, "gate ready (detected): PROVEN") {
		t.Fatalf("gate run rc=%d\nstdout: %s\nstderr: %s", rc, out, errOut)
	}
	rc, g := h.gateJSON()
	if rc != 0 || !g.Proven || len(g.Commands) != 2 || g.Commands[0].State != proof.Proven || g.Commands[1].State != proof.Proven {
		t.Fatalf("after gate run: rc=%d %+v", rc, g)
	}
	// The runs went through the queue like any gate command.
	if r := h.lastRecord(); r.Gate != "ready" || r.Cmd != "sh -c go test ./..." || r.Cache != "miss" {
		t.Fatalf("ledger record: %+v", r)
	}
	if rc, out := h.run("why"); rc != 0 || !strings.Contains(out, "The ready (detected) gate is proven on this tree") {
		t.Fatalf("why rc=%d:\n%s", rc, out)
	}
	// Writing the detection down keeps the gate: same name, same commands.
	// (The new file is an edit to the tree, so it has to be proven again.)
	if rc, _, errOut := h.runSplit("init"); rc != 0 {
		t.Fatalf("init rc=%d\n%s", rc, errOut)
	}
	rc, g2 := h.gateJSON()
	if rc != 1 || g2.Gate != "ready" || len(g2.Commands) != 2 || g2.Commands[0].Cmd != g.Commands[0].Cmd || g2.Commands[1].Cmd != g.Commands[1].Cmd {
		t.Fatalf("after init: rc=%d %+v", rc, g2)
	}
	if _, raw, _ := h.runSplit("gate", "status", "--json"); strings.Contains(raw, "detected") {
		t.Fatalf("heavyq.toml now exists, the gate is no longer detected:\n%s", raw)
	}
}

package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func manPage(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	WriteManPage(&b, "1.2.3", "2026-11-02")
	return b.String()
}

func TestManPageHoldsTheTable(t *testing.T) {
	page := manPage(t)
	if !strings.HasPrefix(page, `.TH HEAVYQ 1 "2026-11-02" "heavyq 1.2.3"`) {
		t.Errorf("bad header: %.80s", page)
	}
	for _, name := range commandNames() {
		if !strings.Contains(page, ".SS "+name+"\n") {
			t.Errorf("no section for %q", name)
		}
	}
	for _, c := range commandTable {
		for _, f := range c.Flags {
			if !strings.Contains(page, roff(f.display())) {
				t.Errorf("%s: flag %s missing", c.Name, f.display())
			}
		}
	}
	for _, want := range []string{".SH EXIT STATUS", "75", "78", ".SH ENVIRONMENT", "HEAVYQ_CONFIG"} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %q", want)
		}
	}
	// Hyphens are escaped, so --max-wait can be searched for and pasted from the page.
	if !strings.Contains(page, `\-\-max\-wait`) {
		t.Error("options are not escaped")
	}
}

// Every line that starts with a dot is a macro we mean; text never does.
func TestManPageHasNoStrayRequests(t *testing.T) {
	known := map[string]bool{".TH": true, ".SH": true, ".SS": true, ".PP": true, ".TP": true, ".nf": true, ".fi": true}
	for i, line := range strings.Split(manPage(t), "\n") {
		if strings.HasPrefix(line, ".") && !known[strings.Fields(line)[0]] {
			t.Errorf("line %d starts a request: %q", i+1, line)
		}
	}
	if got := roff(".dot -x 'q' a\\b"); got != `\&.dot \-x 'q' a\eb` {
		t.Errorf("roff escaping: %q", got)
	}
}

// groff, where installed, must accept the page without warnings.
func TestManPageRendersCleanly(t *testing.T) {
	nroff, err := exec.LookPath("nroff")
	if err != nil {
		t.Skip("nroff not installed")
	}
	path := filepath.Join(t.TempDir(), "heavyq.1")
	if err := os.WriteFile(path, []byte(manPage(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(nroff, "-man", "-ww", path)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("nroff: %v\n%s", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("nroff warnings:\n%s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "--max-wait DUR") {
		t.Error("rendered page lacks --max-wait DUR")
	}
}

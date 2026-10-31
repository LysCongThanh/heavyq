package remote

import (
	"bytes"
	"os/exec"
	"sync"
	"testing"
)

// syncBuffer is a bytes.Buffer safe for one writer goroutine and a reader.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestShellQuoteRoundTrip(t *testing.T) {
	args := []string{"plain", "with space", "it's", `$HOME`, "a\"b", "", "x;y|z", "new\nline"}
	out, err := exec.Command("sh", "-c", "set -- "+ShellJoin(args)+`; for a in "$@"; do printf '[%s]' "$a"; done`).Output()
	if err != nil {
		t.Fatal(err)
	}
	want := ""
	for _, a := range args {
		want += "[" + a + "]"
	}
	if string(out) != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

func TestParseProbe(t *testing.T) {
	r, err := parseProbe("motd noise\nheavyq-probe 4096 1.5 8 h1,l2\n")
	if err != nil || r.Metrics.FreeMB != 4096 || r.Metrics.Cores != 8 || !r.Busy["h1"] || !r.Busy["l2"] || !r.Metrics.Known {
		t.Fatalf("got %+v %v", r, err)
	}
	r, err = parseProbe("heavyq-probe - - 4 -\n")
	if err != nil || r.Metrics.Known || len(r.Busy) != 0 {
		t.Fatalf("unknown metrics: %+v %v", r, err)
	}
	if _, err := parseProbe("garbage"); err == nil {
		t.Fatal("expected error")
	}
}

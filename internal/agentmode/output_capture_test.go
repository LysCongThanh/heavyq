package agentmode

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTailKeepsOnlyTheEnd(t *testing.T) {
	c := NewCapture("", "")
	var out bytes.Buffer
	w := c.Wrap(&out)
	var all strings.Builder
	for i := 0; i < 20000; i++ { // ~280 KB, several times the tail
		line := fmt.Sprintf("line %06d\n", i)
		all.WriteString(line)
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if out.String() != all.String() {
		t.Fatal("the real stream did not get every byte, unchanged")
	}
	tail, cut := c.Tail()
	if !cut || len(tail) != TailBytes {
		t.Fatalf("tail len=%d cut=%v, want %d and true", len(tail), cut, TailBytes)
	}
	if !strings.HasSuffix(all.String(), string(tail)) {
		t.Fatal("tail is not the end of the output")
	}
}

func TestTailOfAHugeSingleWrite(t *testing.T) {
	c := NewCapture("", "")
	w := c.Wrap(&bytes.Buffer{})
	big := append(bytes.Repeat([]byte("a"), 3*TailBytes), []byte("THE END")...)
	_, _ = w.Write(big)
	tail, cut := c.Tail()
	if !cut || len(tail) != TailBytes || !bytes.HasSuffix(tail, []byte("THE END")) {
		t.Fatalf("len=%d cut=%v", len(tail), cut)
	}
}

func TestSmallOutputIsNotCut(t *testing.T) {
	c := NewCapture("", "")
	_, _ = c.Wrap(&bytes.Buffer{}).Write([]byte("hello\n"))
	if tail, cut := c.Tail(); cut || string(tail) != "hello\n" {
		t.Fatalf("tail=%q cut=%v", tail, cut)
	}
}

// failWriter accepts n bytes, then fails, like a closed pipe.
type failWriter struct{ n int }

func (f *failWriter) Write(p []byte) (int, error) {
	if len(p) > f.n {
		n := f.n
		f.n = 0
		return n, fmt.Errorf("broken pipe")
	}
	f.n -= len(p)
	return len(p), nil
}

func TestWriteErrorsAndCountsPassThroughUnchanged(t *testing.T) {
	c := NewCapture("", "")
	n, err := c.Wrap(&failWriter{n: 3}).Write([]byte("abcdef"))
	if n != 3 || err == nil {
		t.Fatalf("n=%d err=%v: the underlying result must be returned as is", n, err)
	}
	if tail, _ := c.Tail(); string(tail) != "abc" {
		t.Fatalf("only what was accepted is recorded, got %q", tail)
	}
}

func TestConcurrentStreamsAreRaceFree(t *testing.T) {
	c := NewCapture(t.TempDir(), testLog("x"))
	var a, b bytes.Buffer
	wa, wb := c.Wrap(&a), c.Wrap(&b)
	var wg sync.WaitGroup
	for _, w := range []interface{ Write([]byte) (int, error) }{wa, wb} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				_, _ = w.Write([]byte("0123456789abcdef\n"))
			}
		}()
	}
	wg.Wait()
	if tail, _ := c.Tail(); len(tail) != TailBytes {
		t.Fatalf("tail %d", len(tail))
	}
	if _, ok := c.Finish(false); ok {
		t.Fatal("Finish(false) must not keep a log")
	}
}

func TestLogIsKeptOnlyWhenAsked(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	r1, r2 := testLog("r1"), testLog("r2")
	c := NewCapture(dir, r1)
	_, _ = c.Wrap(&bytes.Buffer{}).Write([]byte("boom\n"))
	l, ok := c.Finish(true)
	if !ok || l.Path != filepath.Join(dir, r1) || l.Size != 5 || l.Truncated {
		t.Fatalf("log = %+v ok=%v", l, ok)
	}
	if b, err := os.ReadFile(l.Path); err != nil || string(b) != "boom\n" {
		t.Fatalf("log content %q err=%v", b, err)
	}
	if st, _ := os.Stat(l.Path); st.Mode().Perm() != 0o600 {
		t.Fatalf("log mode %v: output may hold secrets", st.Mode().Perm())
	}
	if st, _ := os.Stat(dir); st.Mode().Perm() != 0o700 {
		t.Fatalf("log dir mode %v", st.Mode().Perm())
	}

	c2 := NewCapture(dir, r2)
	_, _ = c2.Wrap(&bytes.Buffer{}).Write([]byte("fine\n"))
	if _, ok := c2.Finish(false); ok {
		t.Fatal("a passing run must not keep a log")
	}
	if _, err := os.Stat(filepath.Join(dir, r2)); !os.IsNotExist(err) {
		t.Fatalf("r2 still there: %v", err)
	}
}

func TestSilentCommandCreatesNoLog(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	c := NewCapture(dir, testLog("quiet"))
	if _, ok := c.Finish(true); ok {
		t.Fatal("no output, no log")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("log dir was created for nothing: %v", err)
	}
}

func TestLogIsCapped(t *testing.T) {
	dir := t.TempDir()
	c := NewCapture(dir, testLog("big"))
	w := c.Wrap(&bytes.Buffer{})
	chunk := bytes.Repeat([]byte("x"), 1<<20)
	for i := 0; i < LogMaxBytes>>20+2; i++ {
		_, _ = w.Write(chunk)
	}
	l, ok := c.Finish(true)
	if !ok || !l.Truncated || l.Size != LogMaxBytes {
		t.Fatalf("log = %+v", l)
	}
	if st, _ := os.Stat(l.Path); st.Size() != LogMaxBytes {
		t.Fatalf("file is %d bytes", st.Size())
	}
}

func TestUnwritableLogDirNeverBreaksTheRun(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c := NewCapture(filepath.Join(blocker, "logs"), testLog("x")) // a file where the directory should be
	var out bytes.Buffer
	if n, err := c.Wrap(&out).Write([]byte("still passes\n")); err != nil || n != 13 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if _, ok := c.Finish(true); ok {
		t.Fatal("no log can exist here")
	}
	if tail, _ := c.Tail(); string(tail) != "still passes\n" {
		t.Fatalf("tail %q: the digest must work without a log", tail)
	}
}

func TestPruneLogs(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	names := make([]string, 5)
	for i := range names {
		names[i] = LogName(now.Add(-time.Duration(i)*time.Hour), 7, "lane")
		p := filepath.Join(dir, names[i])
		_ = os.WriteFile(p, []byte("x"), 0o600)
		_ = os.Chtimes(p, now.Add(-time.Duration(i)*time.Hour), now.Add(-time.Duration(i)*time.Hour))
	}
	oldName := LogName(now.Add(-30*24*time.Hour), 7, "lane")
	old := filepath.Join(dir, oldName)
	_ = os.WriteFile(old, []byte("x"), 0o600)
	_ = os.Chtimes(old, now.Add(-30*24*time.Hour), now.Add(-30*24*time.Hour))
	// Anything that is not one of ours stays, however old and however many.
	for _, n := range []string{"keep.txt", "build.log", "app.log"} {
		p := filepath.Join(dir, n)
		_ = os.WriteFile(p, []byte("x"), 0o600)
		_ = os.Chtimes(p, now.Add(-90*24*time.Hour), now.Add(-90*24*time.Hour))
	}

	PruneLogs(dir, 3, 7*24*time.Hour)
	var left []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		left = append(left, e.Name())
	}
	want := strings.Join([]string{names[2], names[1], names[0], "app.log", "build.log", "keep.txt"}, " ") // directory order
	if got := strings.Join(left, " "); got != want {
		t.Fatalf("left %q, want %q", got, want)
	}
}

// testLog is a valid log file name for a test, unique per call.
func testLog(lane string) string { return LogName(time.Now(), 1, lane) }

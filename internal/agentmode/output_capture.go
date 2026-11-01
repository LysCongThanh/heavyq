package agentmode

import (
	"io"
	"os"
	"path/filepath"
	"sync"
)

const (
	// TailBytes is how much of the end of a command's output is kept for the
	// digest's failure lines.
	TailBytes = 64 << 10
	// LogMaxBytes caps one saved log. A command that prints more keeps its
	// first LogMaxBytes in the file (and its last TailBytes in the digest).
	LogMaxBytes = 4 << 20
)

// Log describes the file a Capture saved.
type Log struct {
	Path      string
	Size      int64
	Truncated bool // output went past LogMaxBytes
}

// Capture tees a command's stdout and stderr, in arrival order, into a bounded
// tail buffer and a log file, without altering what the real streams get: the
// bytes (and their timing) pass through first, and the copy never fails them.
//
// The log is created on the first byte and lives in dir with mode 0600 (output
// can hold secrets). Finish keeps it only when asked to (a failed run), so a
// passing run leaves nothing behind.
type Capture struct {
	mu   sync.Mutex
	tail tail
	dir  string // "" = no log
	file string // file name inside dir (see LogName)
	f    *os.File
	size int64
	over bool
	dead bool // the log could not be created or written: stop trying
}

// NewCapture captures into a tail buffer and, when dir is not empty, into
// dir/file.
func NewCapture(dir, file string) *Capture {
	return &Capture{tail: tail{max: TailBytes}, dir: dir, file: file}
}

// Wrap returns a writer that writes to w and records what w accepted.
func (c *Capture) Wrap(w io.Writer) io.Writer { return teeWriter{c, w} }

type teeWriter struct {
	c *Capture
	w io.Writer
}

func (t teeWriter) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	if n > 0 {
		t.c.record(p[:n])
	}
	return n, err
}

func (c *Capture) record(p []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tail.write(p)
	if c.dir == "" || c.dead || c.over {
		return
	}
	if c.f == nil && !c.open() {
		return
	}
	if room := LogMaxBytes - c.size; int64(len(p)) > room {
		p, c.over = p[:room], true
	}
	n, err := c.f.Write(p)
	c.size += int64(n)
	if err != nil {
		c.dead = true
	}
}

// open creates the log file; callers hold c.mu.
func (c *Capture) open() bool {
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		c.dead = true
		return false
	}
	f, err := os.OpenFile(filepath.Join(c.dir, c.file), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		c.dead = true
		return false
	}
	c.f = f
	return true
}

// Tail is the end of the output (at most TailBytes) and whether earlier bytes
// were dropped, so the first line may be cut.
func (c *Capture) Tail() ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tail.bytes()
}

// Finish closes the log. With keep it stays (older logs are pruned) and is
// described; without, it is removed and ok is false.
func (c *Capture) Finish(keep bool) (Log, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.f == nil {
		return Log{}, false
	}
	path := c.f.Name()
	_ = c.f.Close()
	c.f = nil
	if !keep || (c.dead && c.size == 0) {
		_ = os.Remove(path)
		_ = os.Remove(c.dir) // only succeeds when empty: no directory left for a passing run
		return Log{}, false
	}
	PruneLogs(c.dir, LogKeep, LogMaxAge)
	return Log{Path: path, Size: c.size, Truncated: c.over}, true
}

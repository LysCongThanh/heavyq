package cache

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// Output files are a sequence of chunks: 1 byte stream (1 stdout, 2
// stderr), 4 bytes big-endian length, then the data. Chunks are written in
// the order heavyq received them, so a replay interleaves stdout and stderr
// the way the original run produced them (as far as two pipes allow).
const (
	streamOut byte = 1
	streamErr byte = 2
)

// Recorder tees a command's output to the real streams and to a temp file.
// Recording problems (disk full, output above the entry cap) never affect
// the pass-through; they only make the run uncacheable.
type Recorder struct {
	mu     sync.Mutex
	f      *os.File
	size   int64
	max    int64
	broken error
	stdout io.Writer
	stderr io.Writer
}

// Stdout returns the writer to use as the command's stdout.
func (r *Recorder) Stdout() io.Writer { return teeWriter{r, streamOut} }

// Stderr returns the writer to use as the command's stderr.
func (r *Recorder) Stderr() io.Writer { return teeWriter{r, streamErr} }

type teeWriter struct {
	r      *Recorder
	stream byte
}

func (t teeWriter) Write(p []byte) (int, error) {
	r := t.r
	r.mu.Lock()
	defer r.mu.Unlock()
	dst := r.stdout
	if t.stream == streamErr {
		dst = r.stderr
	}
	n, err := dst.Write(p)
	if r.broken == nil && n > 0 {
		r.record(t.stream, p[:n])
	}
	return n, err
}

// record appends one chunk; callers hold r.mu.
func (r *Recorder) record(stream byte, p []byte) {
	if r.size+int64(len(p))+5 > r.max {
		r.broken = fmt.Errorf("output larger than max_entry_mb (%d MiB)", r.max>>20)
		return
	}
	var hdr [5]byte
	hdr[0] = stream
	binary.BigEndian.PutUint32(hdr[1:], uint32(len(p)))
	if _, err := r.f.Write(hdr[:]); err != nil {
		r.broken = err
		return
	}
	if _, err := r.f.Write(p); err != nil {
		r.broken = err
		return
	}
	r.size += int64(len(p)) + 5
}

// finish closes the temp file and reports why it cannot be stored, if so.
func (r *Recorder) finish() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.f.Close(); err != nil && r.broken == nil {
		r.broken = err
	}
	return r.broken
}

// Abort discards the recording.
func (r *Recorder) Abort() {
	_ = r.finish()
	_ = os.Remove(r.f.Name())
}

// replay writes an open chunk file to stdout/stderr in recorded order.
func replay(f *os.File, stdout, stderr io.Writer) error {
	br := bufio.NewReader(f)
	var hdr [5]byte
	buf := make([]byte, 32*1024)
	for {
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("corrupt cache entry %s: %w", f.Name(), err)
		}
		dst := stdout
		if hdr[0] == streamErr {
			dst = stderr
		}
		n := int64(binary.BigEndian.Uint32(hdr[1:]))
		w, err := io.CopyBuffer(dst, io.LimitReader(br, n), buf)
		if err != nil {
			return err
		}
		if w != n {
			return fmt.Errorf("corrupt cache entry %s: truncated chunk", f.Name())
		}
	}
}

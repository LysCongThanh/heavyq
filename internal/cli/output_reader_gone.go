package cli

import (
	"errors"
	"io"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
)

// `heavyq ... -- cmd | head -20` is a shape agents use all the time (the
// Claude Code hook keeps the pipe outside the wrap). Once head exits, heavyq's
// stdout is a broken pipe. By default the Go runtime then kills heavyq with
// SIGPIPE on its next write there: no ledger line, no digest, and the slot
// freed while the command runs on. Instead heavyq takes SIGPIPE itself (a write
// returns EPIPE) and keeps reading the command's output into the log and the
// digest, supervising the command to its end.

var pipeSignalOnce sync.Once

// survivePipeReaderGone makes a write to a closed pipe return EPIPE instead of
// killing this process. The handler is not inherited: commands started later
// get the default SIGPIPE back on exec.
func survivePipeReaderGone() {
	pipeSignalOnce.Do(func() { signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE) })
}

// outputSink is f as the command's output goes to it: a terminal stays the
// file itself (it stays a terminal for the command); anything else drops what
// is written once its reader has gone, so the command's output keeps being
// read (and kept for the digest) to the end.
func outputSink(f *os.File) io.Writer {
	if st, err := f.Stat(); err != nil || st.Mode()&os.ModeCharDevice != 0 {
		return f
	}
	return &readerGoneSink{f: f}
}

type readerGoneSink struct {
	f    *os.File
	gone atomic.Bool
}

func (s *readerGoneSink) Write(p []byte) (int, error) {
	if s.gone.Load() {
		return len(p), nil
	}
	n, err := s.f.Write(p)
	if errors.Is(err, syscall.EPIPE) {
		s.gone.Store(true)
		return len(p), nil
	}
	return n, err
}

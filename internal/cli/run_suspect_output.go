package cli

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
)

// A command that exits 0 while its output says the shell could not find a
// command is a false green (`sh -c 'missing | tail'` exits with tail's status).
// notFoundLine recognizes those lines. It is deliberately tight: the whole
// line must be a shell's own report, so a test log that merely mentions
// "not found" never matches.
var notFoundLine = []*regexp.Regexp{
	// dash, bash, ksh, busybox: "sh: 1: foo: not found", "bash: line 3: foo: command not found", "bash: foo: command not found"
	regexp.MustCompile(`^-?(?:sh|bash|dash|zsh|ksh|ash|mksh)(?:: line \d+)?: (?:\d+: )?[^\s:]+: (?:command )?not found$`),
	// a script run by bash: "./ci.sh: line 7: foo: command not found"
	regexp.MustCompile(`^[^\s:]+: line \d+: [^\s:]+: command not found$`),
	// zsh: "zsh: command not found: foo"
	regexp.MustCompile(`^-?zsh: command not found: \S+$`),
}

// maxScanLine bounds what is kept of one output line; longer lines are not shell reports.
const maxScanLine = 512

// isNotFoundLine reports whether line (without its newline) is a shell's
// "command not found" report.
func isNotFoundLine(line string) bool {
	line = strings.TrimRight(line, " \t\r")
	if line == "" || len(line) > maxScanLine {
		return false
	}
	for _, re := range notFoundLine {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// suspectScan watches what a command prints for the first shell not-found
// line. It never changes the bytes it passes on.
type suspectScan struct {
	mu      sync.Mutex
	first   string
	streams []*scanWriter
}

// Line is the first not-found line seen, "" when none. Call it once the
// command has ended: it also examines a last line that had no newline.
func (s *suspectScan) Line() string {
	s.mu.Lock()
	streams := s.streams
	s.mu.Unlock()
	for _, sw := range streams {
		sw.flush()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.first
}

func (s *suspectScan) found(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.first == "" {
		s.first = strings.TrimRight(line, " \t\r")
	}
}

// wrap returns w with the scan in front of it. A terminal is left alone: it
// stays a terminal for the command (colors, progress), and a person sees the
// line anyway.
func (s *suspectScan) wrap(w io.Writer) io.Writer {
	if f, ok := w.(*os.File); ok {
		if st, err := f.Stat(); err != nil || st.Mode()&os.ModeCharDevice != 0 {
			return w
		}
	}
	sw := &scanWriter{s: s, w: w}
	s.mu.Lock()
	s.streams = append(s.streams, sw)
	s.mu.Unlock()
	return sw
}

// scanWriter is one stream: it keeps the unfinished last line between writes.
type scanWriter struct {
	mu      sync.Mutex
	s       *suspectScan
	w       io.Writer
	partial []byte
	skip    bool // the current line is too long to be a shell report
}

func (sw *scanWriter) Write(p []byte) (int, error) {
	sw.mu.Lock()
	sw.scan(p)
	sw.mu.Unlock()
	return sw.w.Write(p)
}

func (sw *scanWriter) scan(p []byte) {
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			sw.add(p)
			return
		}
		sw.add(p[:i])
		if !sw.skip && isNotFoundLine(string(sw.partial)) {
			sw.s.found(string(sw.partial))
		}
		sw.partial, sw.skip = sw.partial[:0], false
		p = p[i+1:]
	}
}

func (sw *scanWriter) add(b []byte) {
	if sw.skip {
		return
	}
	if len(sw.partial)+len(b) > maxScanLine+2 {
		sw.partial, sw.skip = sw.partial[:0], true
		return
	}
	sw.partial = append(sw.partial, b...)
}

// flush examines a last line that had no newline (called when the command ended).
func (sw *scanWriter) flush() {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if !sw.skip && isNotFoundLine(string(sw.partial)) {
		sw.s.found(string(sw.partial))
	}
	sw.partial = sw.partial[:0]
}

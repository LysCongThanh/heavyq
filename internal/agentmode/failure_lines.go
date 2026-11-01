package agentmode

import (
	"bytes"
	"regexp"
	"strings"
)

const (
	// MaxFailureLines is how many failure-looking lines a digest quotes.
	MaxFailureLines = 12
	// maxLineBytes clips one quoted line.
	maxLineBytes = 200
	// fallbackLines is how many last lines stand in when nothing looked like a failure.
	fallbackLines = 3
)

// ansiRe matches the colour and cursor escape sequences (CSI) and the title or
// hyperlink sequences (OSC) a command may print. Digest text never has them.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// failureRe matches what a failing run usually prints: Go (`--- FAIL`, `FAIL`,
// `panic:`), compilers (`error:`, `error[E0425]`), runners (`FAILED`, jest's
// `✕`), and Python/JS exceptions (`Traceback`, `AssertionError`, `TypeError:`).
var failureRe = regexp.MustCompile(`\bFAIL(?:ED)?\b|\bpanic:|\berror(?:\[|:)|Error:|AssertionError|Traceback|[✕✗]`)

// locationRe matches a file:line (or file:line:col) reference such as
// `./main.go:10:2`, `src/a.ts:41` or `tests/test_x.py:7`. The extension must
// start with a letter, so addresses like 127.0.0.1:3000 do not count.
var locationRe = regexp.MustCompile(`(?:^|[\s(\[])[\w@./\\-]*[\w@-]\.[A-Za-z]\w{0,5}:\d+(?::\d+)?`)

// FailureLines picks up to max lines of out that look like failures, in the
// order they appeared, and says how many more there were. out is the tail of
// a command's output; cut says its first line may be a fragment, which is
// dropped. When nothing looks like a failure the last few non-empty lines
// stand in (and more is 0), so a failure that prints something unusual still
// shows its end.
//
// Lines are stripped of ANSI sequences and carriage-return redraws, clipped,
// and de-duplicated.
func FailureLines(out []byte, cut bool, max int) (lines []string, more int) {
	rows := bytes.Split(out, []byte("\n"))
	if cut && len(rows) > 1 {
		rows = rows[1:]
	}
	seen := map[string]bool{}
	var last []string
	for _, row := range rows {
		s := cleanLine(row)
		if s == "" {
			continue
		}
		if last = append(last, s); len(last) > fallbackLines {
			last = last[1:]
		}
		if !looksLikeFailure(s) || seen[s] {
			continue
		}
		seen[s] = true
		if len(lines) < max {
			lines = append(lines, clipLine(s))
		} else {
			more++
		}
	}
	if len(lines) == 0 {
		for _, s := range last {
			lines = append(lines, clipLine(s))
		}
	}
	return lines, more
}

func looksLikeFailure(s string) bool {
	if failureRe.MatchString(s) {
		return true
	}
	// A URL's host:port is not a source location.
	return !strings.Contains(s, "://") && locationRe.MatchString(s)
}

// cleanLine drops ANSI, keeps what a carriage return left on screen, and
// trims; the result is "" for a blank line.
func cleanLine(row []byte) string {
	s := ansiRe.ReplaceAllString(string(row), "")
	if i := strings.LastIndexByte(s, '\r'); i >= 0 {
		if rest := strings.TrimSpace(s[i+1:]); rest != "" {
			s = rest
		} else {
			s = s[:i]
		}
	}
	s = strings.Map(func(r rune) rune {
		if r < ' ' && r != '\t' || r == 0x7f {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

func clipLine(s string) string {
	if len(s) <= maxLineBytes {
		return s
	}
	return strings.ToValidUTF8(s[:maxLineBytes-3], "") + "..."
}

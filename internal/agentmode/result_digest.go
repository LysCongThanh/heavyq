package agentmode

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// MaxDigestBytes is the hard cap of a digest, whatever the command printed.
const MaxDigestBytes = 4096

// Verdicts of a digest.
const (
	Pass        = "PASS"
	Fail        = "FAIL"
	Reused      = "REUSED"      // the result of an identical run on this exact tree was replayed
	NotRun      = "NOT RUN"     // heavyq gave up before the command ran (max-wait, remote down)
	Cancelled   = "CANCELLED"   // an operator stopped it
	Interrupted = "INTERRUPTED" // a signal stopped it
)

// Result is everything a digest says. The cli fills it; this package words it.
type Result struct {
	Verdict string
	Cmd     string // already redacted
	Exit    int
	Tree    string // short tree label, "" when unknown
	Dur     time.Duration
	Waited  time.Duration
	// Why is the reason a run did not happen (NotRun, Cancelled, Interrupted).
	Why string
	// Detail is the sentence under a REUSED header: what the old result was.
	Detail string
	// Failures are the quoted failure-looking lines; More counts the rest.
	Failures []string
	More     int
	Log      *Log
	// Next is the one thing to do now; "" for PASS and REUSED-and-passed.
	Next string
}

// Text renders the digest for stderr: a header line, indented body lines, an
// optional `log:` line and, on anything but success, one final `next:` line.
// It is always shorter than MaxDigestBytes: failure lines are dropped from
// the end until it fits.
func (r Result) Text() string {
	for {
		s := r.text()
		if len(s) < MaxDigestBytes || len(r.Failures) == 0 {
			return clipTo(s, MaxDigestBytes-1)
		}
		r.Failures, r.More = r.Failures[:len(r.Failures)-1], r.More+1
	}
}

func (r Result) text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "heavyq: %s %s", r.Verdict, clipCmd(r.Cmd))
	if r.Exit != 0 { // PASS has none; a REUSED failure keeps the old exit code
		fmt.Fprintf(&b, " · exit %d", r.Exit)
	}
	if r.Tree != "" {
		b.WriteString(" · tree " + r.Tree)
	}
	if r.Dur > 0 || r.Verdict == Pass || r.Verdict == Fail {
		b.WriteString(" · " + FormatDur(r.Dur))
	}
	if r.Waited >= 2*time.Second {
		b.WriteString(" (waited " + FormatDur(r.Waited) + ")")
	}
	b.WriteByte('\n')
	for _, s := range []string{r.Why, r.Detail} {
		if s != "" {
			b.WriteString("  " + clipTo(s, 300) + "\n")
		}
	}
	for _, f := range r.Failures {
		b.WriteString("  " + f + "\n")
	}
	if r.More > 0 {
		fmt.Fprintf(&b, "  ... +%d more failure-like lines in the output\n", r.More)
	}
	if r.Log != nil && len(r.Log.Path) <= 512 {
		b.WriteString("log: " + r.Log.Path + " (" + r.Log.describe() + ")\n")
	}
	if r.Next != "" {
		b.WriteString("next: " + clipTo(r.Next, 300) + "\n")
	}
	return b.String()
}

func (l Log) describe() string {
	s := fmt.Sprintf("full output, %.0f KiB", float64(l.Size)/1024)
	if l.Truncated {
		s = fmt.Sprintf("first %d MiB of the output", LogMaxBytes>>20)
	}
	return s
}

// jsonDigest is the schema of the digest under HEAVYQ_OUTPUT=json: one object
// per line, keys always present except where noted (docs/agent-mode.md).
type jsonDigest struct {
	Heavyq   string   `json:"heavyq"` // always "digest"
	Verdict  string   `json:"verdict"`
	Cmd      string   `json:"cmd"`
	Exit     int      `json:"exit"`
	Tree     string   `json:"tree,omitempty"`
	Secs     float64  `json:"secs"`
	Waited   float64  `json:"waited_secs"`
	Why      string   `json:"why,omitempty"`
	Detail   string   `json:"detail,omitempty"`
	Failures []string `json:"failures,omitempty"`
	More     int      `json:"more_failures,omitempty"`
	Log      string   `json:"log,omitempty"`
	Next     string   `json:"next,omitempty"`
}

// JSON renders the digest as one JSON line, under the same cap as Text.
func (r Result) JSON() string {
	for {
		d := jsonDigest{Heavyq: "digest", Verdict: r.Verdict, Cmd: clipCmd(r.Cmd), Exit: r.Exit, Tree: r.Tree,
			Secs: r.Dur.Seconds(), Waited: r.Waited.Seconds(), Why: r.Why, Detail: r.Detail,
			Failures: r.Failures, More: r.More, Next: r.Next}
		if r.Log != nil && len(r.Log.Path) <= 512 {
			d.Log = r.Log.Path
		}
		b, err := json.Marshal(d)
		if (err == nil && len(b) < MaxDigestBytes-1) || len(r.Failures) == 0 {
			return string(b) + "\n"
		}
		r.Failures, r.More = r.Failures[:len(r.Failures)-1], r.More+1
	}
}

// FormatDur words a run time: 0.4s, 12s, 1m5s.
func FormatDur(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return d.Round(time.Second).String()
}

func clipCmd(s string) string { return clipTo(strings.ReplaceAll(s, "\n", " "), 160) }

// clipTo shortens s to at most n bytes on a character boundary, marking the cut.
func clipTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n-3], "") + "..."
}

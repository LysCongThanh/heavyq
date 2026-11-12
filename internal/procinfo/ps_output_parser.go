package procinfo

import (
	"path/filepath"
	"strconv"
	"strings"
)

// psColumns is the column list heavyq asks ps for. Every header is empty
// (the trailing "=") so the output has no header line.
//
// comm is deliberately the LAST column. On macOS it is the full executable
// path and may contain spaces ("/Applications/Google Chrome.app/..."); on
// Linux it is the short name and may be truncated. Because it is last, the
// four numeric columns and the state are split off from the left and
// everything after them is the command, whatever it holds. ucomm (always the short name) would
// avoid the paths but is capped at 16 bytes on macOS, which cuts names such
// as "Google Chrome Helper (Renderer)"; the basename of comm keeps them whole.
const psColumns = "pid=,ppid=,rss=,time=,stat=,comm="

// parsePS turns the output of `ps -axo pid=,ppid=,rss=,time=,stat=,comm=` into
// a process table. Lines that do not start with four plausible numbers and a
// state are skipped: a half-written line must never poison the whole sample.
// A process that exited and waits to be reaped (state Z, a zombie) is left
// out: it uses no CPU and no memory, and counting it inflates every group. CPU
// time is kept in centiseconds, the unit /proc ticks have (USER_HZ = 100).
func parsePS(out []byte) map[int]procSample {
	table := map[int]procSample{}
	for _, line := range strings.Split(string(out), "\n") {
		if pid, s, ok := parsePSLine(line); ok {
			table[pid] = s
		}
	}
	return table
}

func parsePSLine(line string) (int, procSample, bool) {
	rest := line
	var tok [5]string
	for i := range tok {
		tok[i], rest = nextField(rest)
		if tok[i] == "" {
			return 0, procSample{}, false
		}
	}
	pid, err1 := strconv.Atoi(tok[0])
	ppid, err2 := strconv.Atoi(tok[1])
	rss, err3 := strconv.ParseUint(tok[2], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil || pid < 0 || ppid < 0 || exited(tok[4]) {
		return 0, procSample{}, false
	}
	// An unreadable time keeps the process (it still belongs to the tree)
	// with zero CPU, rather than dropping it from Descendants.
	ticks, _ := parseCPUTime(tok[3])
	return pid, procSample{ppid: ppid, ticks: ticks, rssKB: rss, comm: commName(rest)}, true
}

// nextField returns the first whitespace-delimited token of s and the rest
// of s after it (untrimmed on the right, so a command keeps its spaces).
func nextField(s string) (tok, rest string) {
	s = strings.TrimLeft(s, " \t")
	i := strings.IndexAny(s, " \t")
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i:]
}

// commName reduces ps's comm column to a process name: the basename of the
// executable path, so "/usr/sbin/cfprefsd" groups as "cfprefsd" and
// "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" as
// "Google Chrome". A name without a slash (Linux comm, "(sh)" of a
// zombie) is kept as is.
func commName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "?"
	}
	if strings.Contains(name, "/") {
		if base := filepath.Base(name); base != "." && base != "/" {
			return base
		}
	}
	return name
}

// maxCPUSeconds bounds a parsed time (about 31 years) so garbage digits
// cannot overflow the arithmetic.
const maxCPUSeconds = 1 << 30

// parseCPUTime parses ps's cumulative CPU time into centiseconds. Accepted
// shapes: [[dd-]hh:]mm:ss[.cc] (Linux "1-02:03:04", "00:00:07"; macOS
// "0:07.25", "1234:56.78", "1:02:03.50") and bare "ss[.cc]". The fraction
// has any number of digits; only the first two (centiseconds) count.
func parseCPUTime(s string) (uint64, bool) {
	var days uint64
	if d, rest, found := strings.Cut(s, "-"); found {
		n, ok := parseUint(d)
		if !ok {
			return 0, false
		}
		days, s = n, rest
	}
	main, frac, _ := strings.Cut(s, ".")
	parts := strings.Split(main, ":")
	if len(parts) > 3 || main == "" {
		return 0, false
	}
	var secs uint64
	for _, p := range parts { // hh:mm:ss, mm:ss or ss, most significant first
		n, ok := parseUint(p)
		if !ok {
			return 0, false
		}
		secs = secs*60 + n
	}
	secs += days * 86400
	if secs > maxCPUSeconds {
		return 0, false
	}
	var cs uint64
	if frac != "" {
		if _, ok := parseUint(frac); !ok {
			return 0, false
		}
		cs, _ = parseUint((frac + "00")[:2]) // ".5" -> 50, ".257" -> 25
	}
	return secs*100 + cs, true
}

// parseUint accepts plain decimal digits only (no sign, no spaces).
func parseUint(s string) (uint64, bool) {
	if s == "" || len(s) > 12 {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	return n, err == nil
}

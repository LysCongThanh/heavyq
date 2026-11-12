package fleet

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/procinfo"
	"github.com/LysCongThanh/heavyq/internal/redact"
)

// Ticket is one live ticket of any queue.
type Ticket struct {
	Queue   string    `json:"queue"`
	Lane    string    `json:"lane"`
	Prio    int       `json:"prio"`
	Pid     int       `json:"pid"`
	Since   time.Time `json:"since"`
	Running bool      `json:"running"`
	Machine string    `json:"machine,omitempty"` // where it runs (running tickets that say so)
	Slot    string    `json:"slot,omitempty"`
	Claimed time.Time `json:"claimed,omitzero"` // when it started running, when the ticket says so
	Cmd     string    `json:"cmd,omitempty"`    // redacted
}

// TicketsRead is the content of one tickets dir.
type TicketsRead struct {
	Tickets []Ticket // queue order: priority, then age
	Stale   int      // tickets whose submitting process is gone (the queue prunes them, heavyq only counts)
}

// Bounds of a tickets dir read: a runaway dir must not stall the report.
const (
	maxTickets     = 4096
	maxTicketBytes = 4096
)

// foreignTicketName is "<prio>-<20-digit unix ns>-<pid>-<lane>", plus ".run" while running. A lane with
// any other character is not a ticket heavyq will show.
var foreignTicketName = regexp.MustCompile(`^([1-9])-([0-9]{20})-([0-9]{1,10})-([A-Za-z0-9_.-]{1,128}?)(\.run)?$`) // lazy: "api.run" is lane api, running

// plainWord is an acceptable machine or slot name read from a ticket.
var plainWord = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ReadTickets lists the tickets of a foreign queue's directory. alive says whether a submitting pid still
// runs on this machine (nil: procinfo.Default.Running). Content is read only for the KEY=VALUE words
// machine=, slot= and cmd= (cmd takes the rest of the line), at most maxTicketBytes per file.
func ReadTickets(dir string, alive func(pid int) bool) (TicketsRead, error) {
	var out TicketsRead
	if alive == nil {
		alive = procinfo.Default.Running
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return out, err
	}
	running := map[string]bool{}
	for _, e := range ents {
		if n := e.Name(); strings.HasSuffix(n, ".run") {
			running[strings.TrimSuffix(n, ".run")] = true
		}
	}
	for _, e := range ents {
		if len(out.Tickets) >= maxTickets {
			break
		}
		m := foreignTicketName.FindStringSubmatch(e.Name())
		if m == nil || !e.Type().IsRegular() || (m[5] == "" && running[e.Name()]) {
			continue // not a ticket, or the waiting twin of a ticket being claimed: the .run file wins
		}
		prio, _ := strconv.Atoi(m[1])
		ns, _ := strconv.ParseInt(m[2], 10, 64)
		pid, _ := strconv.Atoi(m[3])
		t := Ticket{Lane: m[4], Prio: prio, Pid: pid, Since: time.Unix(0, ns), Running: m[5] != ""}
		if !alive(pid) {
			out.Stale++
			continue
		}
		t.Machine, t.Slot, t.Claimed, t.Cmd = ticketWords(filepath.Join(dir, e.Name()))
		out.Tickets = append(out.Tickets, t)
	}
	sort.SliceStable(out.Tickets, func(i, j int) bool {
		a, b := out.Tickets[i], out.Tickets[j]
		if a.Prio != b.Prio {
			return a.Prio < b.Prio
		}
		return a.Since.Before(b.Since)
	})
	return out, nil
}

// ticketWords reads machine=, slot=, claimed= (unix seconds) and cmd= from a ticket's first line.
func ticketWords(path string) (machine, slot string, claimed time.Time, cmd string) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", time.Time{}, ""
	}
	defer f.Close()
	buf := make([]byte, maxTicketBytes)
	n, _ := f.Read(buf)
	line, _, _ := strings.Cut(string(buf[:n]), "\n")
	if i := strings.Index(line, "cmd="); i >= 0 && (i == 0 || line[i-1] == ' ') {
		cmd = redact.Command(strings.TrimSpace(line[i+4:]))
		line = line[:i]
	}
	for _, w := range strings.Fields(line) {
		k, v, _ := strings.Cut(w, "=")
		if !plainWord.MatchString(v) {
			continue // shown in tables and matched to slot names: anything else is noise
		}
		switch k {
		case "machine":
			machine = v
		case "slot":
			slot = v
		case "claimed":
			if sec, err := strconv.ParseInt(v, 10, 64); err == nil && sec > 0 {
				claimed = time.Unix(sec, 0)
			}
		}
	}
	return machine, slot, claimed, cmd
}

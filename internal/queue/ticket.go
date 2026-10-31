// Package queue stores tickets as files in a queue directory.
//
// A waiting ticket is the file "<prio>-<20-digit unix nanos>-<pid>-<lane>";
// its lexical order is the queue order (priority first, then FIFO). When a
// ticket claims a slot it is renamed to "<name>.run" and records the machine
// and slot it holds. Tickets whose process died are pruned by anyone.
package queue

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Ticket is one queued or running job.
type Ticket struct {
	Pid        int       `json:"pid"`
	Start      string    `json:"start"` // process start token (pid reuse guard)
	Lane       string    `json:"lane"`
	Prio       int       `json:"prio"`
	Light      bool      `json:"light,omitempty"`
	Target     string    `json:"target"`     // local | auto | <remote name>
	Candidates []string  `json:"candidates"` // machines this ticket may run on
	Since      time.Time `json:"since"`
	Cmd        string    `json:"cmd"`

	Machine   string    `json:"machine,omitempty"`
	Slot      string    `json:"slot,omitempty"`
	ClaimedAt time.Time `json:"claimed_at,omitzero"`

	Name    string `json:"-"` // file name without ".run"
	Running bool   `json:"-"`
}

const (
	runSuffix = ".run"
	maxCmdLen = 200
	maxLane   = 40
)

var (
	ticketName = regexp.MustCompile(`^[1-9]-[0-9]{20}-[0-9]+-[A-Za-z0-9_.-]+$`)
	laneBad    = regexp.MustCompile(`[^A-Za-z0-9_.-]`)
)

// SanitizeLane makes a lane name safe for file names and remote labels.
func SanitizeLane(s string) string {
	// Must start with a letter or digit: lanes are also remote job labels.
	s = strings.TrimLeft(laneBad.ReplaceAllString(s, "_"), "._-")
	if len(s) > maxLane {
		s = s[:maxLane]
	}
	if s == "" {
		return "lane"
	}
	return s
}

// TruncateCmd renders argv for display, bounded in length.
func TruncateCmd(argv []string) string {
	s := strings.Join(argv, " ")
	if len(s) > maxCmdLen {
		s = strings.ToValidUTF8(s[:maxCmdLen-3], "") + "..."
	}
	return s
}

func (t *Ticket) fileName(now time.Time) string {
	return fmt.Sprintf("%d-%020d-%d-%s", t.Prio, now.UnixNano(), t.Pid, t.Lane)
}

// Uses reports whether the ticket may run on machine.
func (t *Ticket) Uses(machine string) bool {
	for _, c := range t.Candidates {
		if c == machine {
			return true
		}
	}
	return false
}

// RemoteOnly reports whether the ticket cannot run locally.
func (t *Ticket) RemoteOnly() bool { return len(t.Candidates) > 0 && !t.Uses("local") }

func (t *Ticket) encode() ([]byte, error) { return json.Marshal(t) }

func decodeTicket(name string, b []byte) (*Ticket, error) {
	var t Ticket
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	t.Running = strings.HasSuffix(name, runSuffix)
	t.Name = strings.TrimSuffix(name, runSuffix)
	return &t, nil
}

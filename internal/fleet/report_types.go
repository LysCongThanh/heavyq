package fleet

import "time"

// ReportSchema is the version of the `heavyq fleet --json` document. Fields may be added within it.
const ReportSchema = 1

// Machine roles.
const (
	RoleLocal       = "local"        // this machine
	RoleRemote      = "remote"       // a [[remote]] heavyq runs on
	RoleObserveOnly = "observe_only" // a [[remote]] with observe_only = true: probed, never used
)

// Queue kinds.
const (
	KindHeavyq  = "heavyq"
	KindForeign = "foreign"
)

// Report is the whole fleet at one instant, plus a window of history.
type Report struct {
	Schema      int       `json:"schema"`
	GeneratedAt time.Time `json:"generated_at"`
	Since       time.Time `json:"since"`    // start of the history window
	WindowS     float64   `json:"window_s"` // its length in seconds
	Machines    []Machine `json:"machines"`
	Queues      []Queue   `json:"queues"`
	Notes       []string  `json:"notes"` // what could not be read (a missing or unparsable history, a partial one, a missing tickets dir)
}

// Machine is one machine of the fleet.
type Machine struct {
	Name        string     `json:"name"`
	Role        string     `json:"role"`
	Reachable   bool       `json:"reachable"`
	Error       string     `json:"error,omitempty"`   // why it is unreachable
	Partial     string     `json:"partial,omitempty"` // what the probe could not measure in time, and why; the numbers are there
	Cores       int        `json:"cores"`
	Load1       float64    `json:"load1"`
	LoadPerCore float64    `json:"load_per_core"`
	FreeMB      int        `json:"free_mb"`
	TotalMB     int        `json:"total_mb"`
	Pressure    *Pressure  `json:"pressure,omitempty"`
	Overload    bool       `json:"overload"`
	OverloadWhy []string   `json:"overload_why"`
	Slots       []SlotUse  `json:"slots"`
	Top         []Group    `json:"top"` // busiest process groups, attributed to a queue and lane, or "outside"
	OutsideRSS  int        `json:"outside_rss_mb"`
	Cap         *WorkerCap `json:"worker_cap,omitempty"`
	CapUnknown  string     `json:"worker_cap_unknown,omitempty"` // why there is no worker cap although a queue uses the machine
	Window      Stats      `json:"window"`                       // runs of every queue that ended on this machine in the window
}

// SlotUse is one queue's slots on a machine.
type SlotUse struct {
	Owner  string `json:"owner"`
	Busy   int    `json:"busy"`
	Total  int    `json:"total"`            // 0 when unknown
	Unread bool   `json:"unread,omitempty"` // its slot locks were not read (a partial probe): busy is only its running tickets
}

// Queue is one queue: heavyq's own or a foreign one.
type Queue struct {
	Name      string         `json:"name"`
	Kind      string         `json:"kind"`
	Window    Stats          `json:"window"`
	Machines  []MachineStats `json:"machines"` // the window per machine, most runs first
	Running   []LiveTicket   `json:"running"`
	Waiting   []LiveTicket   `json:"waiting"` // longest wait first
	Stale     int            `json:"stale"`   // tickets whose process is gone
	Truncated bool           `json:"history_truncated"`
}

// MachineStats is a queue's window on one machine.
type MachineStats struct {
	Machine string `json:"machine"`
	Window  Stats  `json:"window"`
}

// LiveTicket is a running or waiting ticket now.
type LiveTicket struct {
	Lane    string  `json:"lane"`
	Prio    int     `json:"prio"`
	Machine string  `json:"machine,omitempty"`
	Slot    string  `json:"slot,omitempty"`
	WaitS   float64 `json:"wait_s"`          // waiting so far, or until it started
	RunS    float64 `json:"run_s,omitempty"` // running so far, when known
	Cmd     string  `json:"cmd,omitempty"`   // redacted
}

// MachineInput is what the caller measured of one machine.
type MachineInput struct {
	Name        string
	Role        string
	Probe       *Probe // nil when Err is set
	Err         error
	HeavyqSlots int // heavyq's heavy slots there (0 for an observed machine)
	HeavyqTotal int // heavyq's heavy + light slots there
}

// QueueInput is what the caller read of one queue.
type QueueInput struct {
	Name, Kind string
	Runs       []Run    // the window, Class set
	Tickets    []Ticket // live tickets; Machine already mapped to heavyq names
	Stale      int
	Truncated  bool
	History    bool // the run history was read (it may hold no run): the worker cap counts slots used, not configured
	// HeavySlots and TotalSlots are this queue's slots per machine (heavy, heavy + light), when known.
	HeavySlots map[string]int
	TotalSlots map[string]int
}

// Inputs is everything Assemble needs.
type Inputs struct {
	Now, Since time.Time
	Machines   []MachineInput
	Queues     []QueueInput
	TopN       int // process groups per machine (0: DefaultTopN)
	Notes      []string
}

// DefaultTopN is how many process groups a machine lists.
const DefaultTopN = 5

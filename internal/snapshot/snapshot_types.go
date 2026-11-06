// Package snapshot defines the machine-readable state of heavyq: the output
// of `heavyq status --json` and the payload the dashboard streams.
//
// The shape is a public contract (schema 1). Fields may be added; existing
// fields keep their name, type and meaning until the schema number changes.
package snapshot

import "time"

// Schema is the version of the Snapshot shape.
const Schema = 1

// Snapshot is the whole state of the queue at one instant.
type Snapshot struct {
	Schema      int       `json:"schema"`
	GeneratedAt time.Time `json:"generated_at"`
	Version     string    `json:"version"` // heavyq version
	Host        string    `json:"host"`    // local host name
	Config      Config    `json:"config"`
	Machines    []Machine `json:"machines"`
	Running     []Running `json:"running"`
	Waiting     []Waiting `json:"waiting"`   // turn order
	Coalesced   []Group   `json:"coalesced"` // identical runs sharing one result
	CPU         CPU       `json:"cpu"`
	// Story is the state of the queue as sentences (why agents wait, what
	// holds the RAM, when the bypass applies), computed by Story at
	// GeneratedAt. Added within schema 1: older readers ignore it.
	Story []Sentence `json:"story"`
	// Gates is the proof state of the main gate of each recently active
	// lane's work tree, and Exceptions what needs a human right now (see
	// ComputeExceptions). Both added within schema 1; empty arrays when
	// there is nothing to say.
	Gates      []LaneGate  `json:"gates"`
	Exceptions []Exception `json:"exceptions"`
	// Pools are the named pools (ports, databases, ...) with who holds what;
	// an empty array when none is configured. Added within schema 1.
	Pools []Pool `json:"pools"`
	// Items is what each lane holds or waits on in the work items
	// (`heavyq item`): claimed, proven (not yet done) and blocked items. Added
	// within schema 1; an empty array when no lane has any.
	Items []LaneItems `json:"items"`
}

// Config is the part of the configuration a viewer needs to interpret values.
type Config struct {
	Path           string  `json:"path"` // "" when running on defaults
	ResourceGate   bool    `json:"resource_gate"`
	ForceAfterS    float64 `json:"force_after_s"` // anti-starvation bypass delay
	HoldForS       float64 `json:"hold_for_s"`
	PollS          float64 `json:"poll_s"`
	RemoteDownMaxS float64 `json:"remote_down_max_s"`
}

// Machine is the local workstation or one remote.
type Machine struct {
	Name         string  `json:"name"` // "local" or the remote's name
	Reachable    bool    `json:"reachable"`
	Paused       bool    `json:"paused"`        // no new admissions (dashboard / `heavyq pause`)
	MetricsKnown bool    `json:"metrics_known"` // false: platform cannot be measured
	Cores        int     `json:"cores"`
	Load1        float64 `json:"load1"`
	MaxLoad      float64 `json:"max_load"`    // max_load_ratio x cores: the gate's load limit
	FreeMB       int     `json:"free_mb"`     // MemAvailable
	TotalMB      int     `json:"total_mb"`    // 0 when unknown (remotes)
	ReservedMB   int     `json:"reserved_mb"` // promised to jobs started < hold_for ago
	JobMB        int     `json:"job_mb"`
	LightMB      int     `json:"light_mb"`
	ReserveMB    int     `json:"reserve_mb"`
	Slots        []Slot  `json:"slots"`
	// Admits is "" when a heavy job could start here now, else why not.
	Admits string `json:"admits"`
}

// Slot states.
const (
	SlotFree    = "free"
	SlotHeld    = "held"    // held by a queue ticket (Ticket/Lane set)
	SlotOutside = "outside" // locked by something outside the queue
)

// Slot is one heavy or light execution slot.
type Slot struct {
	Name   string `json:"name"` // heavy-1, light-1, ...
	Kind   string `json:"kind"` // heavy | light
	State  string `json:"state"`
	Ticket string `json:"ticket,omitempty"`
	Lane   string `json:"lane,omitempty"`
}

// Pool is a named pool of values that runs hold while they run.
type Pool struct {
	Name     string `json:"name"`
	Capacity int    `json:"capacity"` // values a run can get (a port pool leaves out the ephemeral range)
	Held     int    `json:"held"`     // values held by running tickets
	// Holders lists the held values, in the pool's order.
	Holders []PoolHolder `json:"holders"`
}

// PoolHolder is one held pool value.
type PoolHolder struct {
	Value  string `json:"value"`
	Lane   string `json:"lane"`
	Ticket string `json:"ticket"` // ticket id of the holder
}

// Ticket is what running and waiting entries share.
type Ticket struct {
	ID         string    `json:"id"` // stable for the ticket's life; used by control actions
	Pid        int       `json:"pid"`
	Lane       string    `json:"lane"`
	Prio       int       `json:"prio"` // 1 runs first
	Light      bool      `json:"light"`
	Target     string    `json:"target"` // local | auto | <remote>
	Candidates []string  `json:"candidates"`
	Cmd        string    `json:"cmd"`   // secrets redacted
	Since      time.Time `json:"since"` // enqueued
	// Needs are the pool values it asks for ("pg", "port:2"); absent without any.
	Needs []string `json:"needs,omitempty"`
	// Pools are the values a running ticket holds, by pool.
	Pools map[string][]string `json:"pools,omitempty"`
	// With are the pinned --with sibling repositories ("name@sha12"); absent without any.
	With  []string `json:"with,omitempty"`
	WaitS float64  `json:"wait_s"`
	// CancelRequested: an operator asked to stop it; its process applies
	// the request within about a poll interval.
	CancelRequested bool `json:"cancel_requested"`
}

// Running is a ticket that holds a slot.
type Running struct {
	Ticket
	Machine   string    `json:"machine"`
	Slot      string    `json:"slot"`
	ClaimedAt time.Time `json:"claimed_at"`
	RunS      float64   `json:"run_s"`
	Forced    bool      `json:"forced"`  // admitted by the anti-starvation bypass
	CPUPct    float64   `json:"cpu_pct"` // whole process tree; 100 = one core
	RSSMB     int       `json:"rss_mb"`  // whole process tree (local only)
}

// Waiting is a ticket in the queue.
type Waiting struct {
	Ticket
	Position int `json:"position"` // 1 = head
	// Reasons maps each candidate machine to why the ticket cannot start
	// there now ("" = nothing blocks it; it starts on its next poll).
	Reasons map[string]string `json:"reasons"`
	// BypassInS: seconds until the anti-starvation bypass (which skips the
	// load check, never RAM or a pool) applies to this ticket. Head of line
	// only; -1 when it would not help: several candidate machines, or the
	// ticket waits for RAM or a pool.
	BypassInS float64 `json:"bypass_in_s"`
}

// Party is one side of a coalesced group.
type Party struct {
	Lane  string    `json:"lane"`
	Pid   int       `json:"pid"`
	Cmd   string    `json:"cmd"`
	Since time.Time `json:"since"`
}

// Group is a leader run with the identical runs waiting for its result.
type Group struct {
	Leader    *Party  `json:"leader"` // nil: leader not visible (finishing)
	Followers []Party `json:"followers"`
}

// CPU attributes current CPU and memory to lanes and to processes outside
// the queue (local machine only).
type CPU struct {
	Available    bool    `json:"available"` // false without /proc
	IntervalMS   int     `json:"interval_ms"`
	Lanes        []Usage `json:"lanes"`   // by lane of running tickets
	Outside      []Usage `json:"outside"` // processes not started by heavyq, by executable
	OutsideTotal Usage   `json:"outside_total"`
}

// Usage is the CPU and memory of a group of processes.
type Usage struct {
	Name   string  `json:"name"` // lane or executable name
	Procs  int     `json:"procs"`
	CPUPct float64 `json:"cpu_pct"`
	RSSMB  int     `json:"rss_mb"`
}

// Package history is the run ledger: one JSON line per finished run, bound to
// the code it ran on (commit, tree, dirty) and to who ran it (session, item,
// gate). Lines are appended with one O_APPEND write and rotated by month.
package history

import "time"

// SchemaVersion is written to every new line as "v". Lines without "v" were
// written before the ledger carried identity (v1) and still load.
const SchemaVersion = 2

// Record is one finished (or interrupted) run.
type Record struct {
	// V is the schema of the line (2). 0 means a v1 line, see Version.
	V int `json:"v,omitempty"`
	// ID is a ULID: unique, and ordered by creation time at millisecond
	// resolution only (see NewID). Order records by file position or End.
	ID       string    `json:"id,omitempty"`
	End      time.Time `json:"end"`
	Machine  string    `json:"machine"`
	Lane     string    `json:"lane"`
	Prio     int       `json:"prio"`
	Light    bool      `json:"light,omitempty"`
	WaitSecs float64   `json:"wait_s"`
	RunSecs  float64   `json:"run_s"`
	Exit     int       `json:"exit"`
	Cmd      string    `json:"cmd"`
	// CmdHash is the sha256 of the exact argv, taken before the redaction and
	// the cut that Cmd went through (see CmdHash). "" on older lines.
	CmdHash string `json:"cmd_hash,omitempty"`
	// RunID is the identity of the run (internal/runident): tree, exact argv,
	// subdir, allowlisted env values and base ref commit, hashed. Cache, proof,
	// flaky and loop detection compare it. "" on older lines, and on runs whose
	// tree is unknown.
	RunID string `json:"run_id,omitempty"`
	// Cache is "hit", "coalesced" (reused an identical in-flight run),
	// "miss" (ran and was considered for storing) or "skip" (--cache given
	// but not cacheable); "" without --cache.
	Cache     string `json:"cache,omitempty"`
	CacheNote string `json:"cache_note,omitempty"`
	// Result is "" for a command that ran to its end, else OutcomeGaveUp
	// (left the queue without running), OutcomeCancelled (stopped on request)
	// or OutcomeInterrupted (a signal stopped heavyq). Older lines have none.
	Result string `json:"result,omitempty"`
	// Forced: admitted by the anti-starvation bypass of the resource gate.
	Forced bool `json:"forced,omitempty"`
	// Needs are the pool values the run asked for, comma-separated ("port:2,pg"),
	// and Pools the values it held, "pool=v1,v2 pool=v" in the order of Needs
	// (empty when it never ran). Flat strings, so the ledger line stays flat and
	// Record stays comparable. Added within v2: older lines have neither.
	Needs string `json:"needs,omitempty"`
	Pools string `json:"pools,omitempty"`
	// The machine as it was when the run was admitted (local: metrics.Local,
	// remote: the probe placement used), with the limits the resource gate
	// applied to it. All of it is absent when the machine could not be
	// measured, so Cores > 0 marks a reading: a zero Load1 or FreeMB beside it
	// is real. See Loaded.
	Load1   float64 `json:"load1,omitempty"`
	FreeMB  int     `json:"free_mb,omitempty"`
	Cores   int     `json:"cores,omitempty"`
	MaxLoad float64 `json:"max_load,omitempty"` // load1 at or above this stops the gate (max_load_ratio x cores)
	JobMB   int     `json:"job_mb,omitempty"`   // RAM budget of the job (job_mb, or light_mb for --light)
	// CPUSecs and MaxRSSMB are measured for local runs (user+system time of
	// the command and its waited-for children, largest resident set).
	CPUSecs  float64 `json:"cpu_s,omitempty"`
	MaxRSSMB int     `json:"max_rss_mb,omitempty"`

	// Who ran it and why (flags --session, --item, --gate or the environment).
	Session string `json:"session,omitempty"`
	Item    string `json:"item,omitempty"`
	Gate    string `json:"gate,omitempty"`
	// Agent ("claude-code/2.1.90") and Model ("claude-opus-5") are the coding agent
	// and model of the session, joined from its session meta record (internal/
	// sessionmeta) when the run was written. They are what baselines are
	// segmented on at a change point. Absent for a plain terminal, and on lines
	// written before they existed.
	Agent string `json:"agent,omitempty"`
	Model string `json:"model,omitempty"`

	// The code it ran on. All empty outside a git repository.
	//
	// Repo identifies the repository (hash of the absolute git common dir, the
	// same for every work tree of it). Worktree and Subdir say where the
	// command ran. Commit is HEAD, or for a remote run the shipped commit.
	// Tree identifies the exact files: HEAD's tree id when the work tree is
	// clean, "<HEAD tree>+<digest of the edits>" when it is dirty. It is empty
	// when the work tree changed while a local command ran, so that run can
	// never be taken as proof of either tree. Dirty (absent = false) reports
	// uncommitted or untracked files; remote runs are never dirty.
	Repo     string `json:"repo,omitempty"`
	Worktree string `json:"worktree,omitempty"`
	Subdir   string `json:"subdir,omitempty"`
	Commit   string `json:"commit,omitempty"`
	Tree     string `json:"tree,omitempty"`
	Dirty    bool   `json:"dirty,omitempty"`
}

// Outcomes of a record.
const (
	OutcomeRan         = "ran"
	OutcomeGaveUp      = "gave_up"
	OutcomeCancelled   = "cancelled"
	OutcomeInterrupted = "interrupted"
)

// Outcome classifies r: ran, gave_up, cancelled or interrupted (signal).
func (r Record) Outcome() string {
	if r.Result != "" {
		return r.Result
	}
	return OutcomeRan
}

// Version is the schema of r: 1 for lines written before "v" existed.
func (r Record) Version() int {
	if r.V == 0 {
		return 1
	}
	return r.V
}

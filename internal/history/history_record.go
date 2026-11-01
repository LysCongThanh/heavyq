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
	// ID is a ULID: unique, and sorts by creation time (see NewID).
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
	// CPUSecs and MaxRSSMB are measured for local runs (user+system time of
	// the command and its waited-for children, largest resident set).
	CPUSecs  float64 `json:"cpu_s,omitempty"`
	MaxRSSMB int     `json:"max_rss_mb,omitempty"`

	// Who ran it and why (flags --session, --item, --gate or the environment).
	Session string `json:"session,omitempty"`
	Item    string `json:"item,omitempty"`
	Gate    string `json:"gate,omitempty"`

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

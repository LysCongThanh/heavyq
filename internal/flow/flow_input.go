package flow

import (
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/itemstatus"
	"github.com/LysCongThanh/heavyq/internal/lease"
)

// Defaults of the derivation.
const (
	// DefaultWindow is the rolling window of the aggregates when there is no
	// run span: the last four hours of wall-clock time.
	DefaultWindow = 4 * time.Hour
	// ActiveGap: a session that ran something at most this long ago, and gaps
	// between its runs up to this long, count as working. Longer silences are
	// idle and belong to no state.
	ActiveGap = 10 * time.Minute
	// SignificantShare is the share of lead time at which the constraint is news.
	SignificantShare = 0.30
	// blockMaxAge: a hook block this old with no answer is an abandoned session,
	// not a person being waited on.
	blockMaxAge = 24 * time.Hour
)

// ItemFact is one work item as itemstatus derived it, plus what is needed to
// tell its proof runs from its other runs in the ledger.
type ItemFact struct {
	Status itemstatus.Status
	// Gate and ProofCmds name the item's proof: a ledger run tagged with the
	// gate, or whose command line is one of ProofCmds (redacted, as the ledger
	// holds them), is proving. Every run tagged with any gate is proving too.
	Gate      string
	ProofCmds []string
}

// Event kinds written by the hooks (internal/flowstate keeps them).
const (
	EventBlocked     = "blocked"      // a person is needed: permission, idle prompt, input
	EventCleared     = "cleared"      // the lane's agent is working again (its next tool call)
	EventStopFailure = "stop_failure" // the turn ended on an API error (rate_limit, overloaded, ...)
	EventModelSwitch = "model_switch" // PostModelSwitch: a change point for baselines
)

// Classes of a blocked event, strongest first: the reason shown is the one of
// the strongest class seen while a block is open.
const (
	ClassPermissionRequest = "permission_request" // PermissionRequest hook: fires at once
	ClassPermissionPrompt  = "permission_prompt"  // Notification, after ~6 s without typing
	ClassAgentNeedsInput   = "agent_needs_input"
	ClassIdlePrompt        = "idle_prompt" // Notification, ~60 s after a response
	ClassStopFailure       = "stop_failure"
)

var classRank = map[string]int{
	ClassPermissionRequest: 5, ClassPermissionPrompt: 4, ClassAgentNeedsInput: 3, ClassIdlePrompt: 2, ClassStopFailure: 1,
}

// Event is one observation by a hook, in order of At. Reason is already
// redacted (it may contain a command line).
type Event struct {
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"`
	Lane    string    `json:"lane"`
	Session string    `json:"session,omitempty"`
	Class   string    `json:"class,omitempty"`  // blocked: one of the Class* values
	Reason  string    `json:"reason,omitempty"` // blocked: what is being waited on
	Detail  string    `json:"detail,omitempty"` // stop_failure: the error; model_switch: the new model
}

// External is a span some optional collector derived for an item from sources
// this package never touches: gh (ci_wait, merge_wait) and git
// (conflict_rework). It is the typed hook for them: a collector returns
// []External, the caller puts it in Input.External, and the spans are painted
// into the item's timeline like any other. Without a collector those states
// simply never appear.
type External struct {
	Item string
	Span Span
	// Files are the paths a conflict-resolving merge conflicted in (a
	// ConflictRework span); empty for the other states.
	Files []string
}

// Collector is the signature of such a collector. repo is the work tree root;
// the spans it returns may start before from.
type Collector func(repo string, from, to time.Time) ([]External, error)

// SessionInfo is the "session" block of the snapshot: what the status line
// last said about the model, the agent and the rate-limit headroom.
type SessionInfo struct {
	Model        string      `json:"model"`
	AgentVersion string      `json:"agent_version"`
	RateLimits   *RateLimits `json:"rate_limits,omitempty"`
}

// RateLimits are the used percentages (0..100) of the subscription windows.
type RateLimits struct {
	FiveHourPct *float64 `json:"five_hour_pct,omitempty"`
	SevenDayPct *float64 `json:"seven_day_pct,omitempty"`
}

// Input is everything Build looks at. Records are this repository's ledger
// runs and Events its lease events, both oldest first and already bounded by
// the caller to somewhat more than the window.
type Input struct {
	Now    time.Time
	Window time.Duration // 0: DefaultWindow
	Repo   string
	Goal   string

	Records  []history.Record
	Leases   []lease.Event
	Items    []ItemFact
	Hooks    []Event
	External []External
	Session  *SessionInfo
	// Baseline is what the constraint finder has learned is normal; nil on
	// the first ever computation. Snapshot.Baseline is the next one to keep.
	Baseline *Baseline
	// Run is the open run span of internal/runspan, nil when none is.
	Run *RunInfo
}

func (in Input) window() time.Duration {
	if in.Window > 0 {
		return in.Window
	}
	return DefaultWindow
}

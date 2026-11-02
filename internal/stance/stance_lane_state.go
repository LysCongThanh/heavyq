package stance

import (
	"time"
)

// MaxActivationsPerSession caps how often one stance may be entered by
// evidence or keyword in one session of a lane. `heavyq stance enter` is an
// explicit request and is counted but never refused.
const MaxActivationsPerSession = 3

// Why an exit happened (Event.Reason of an exit).
const (
	ExitDecisionRecord = "decision_record" // a valid decision record exists
	ExitTimebox        = "timebox"         // the stance outlived exit.timebox_minutes
	ExitManual         = "manual"          // the stance has no exit condition, or it was left on request
	ExitForced         = "forced"          // `stance exit --force` over an unmet condition
	ExitReplaced       = "replaced"        // another stance was entered
	ExitOff            = "off"             // `stance off`
)

// Active is the stance a lane is in.
type Active struct {
	Stance  string    `json:"stance"`
	Trigger Trigger   `json:"trigger"`
	Why     string    `json:"why"` // the evidence, as injected
	Item    string    `json:"item,omitempty"`
	Session string    `json:"session,omitempty"`
	Since   time.Time `json:"since"`
	// Expires is Since plus exit.timebox_minutes; zero when the stance has no timebox.
	Expires time.Time `json:"expires,omitzero"`
}

// State is the stance state of one lane, one small file in the git common dir.
type State struct {
	V    int    `json:"v"`
	Lane string `json:"lane"`
	// Muted: `heavyq stance off`. Nothing enters a muted lane except an
	// explicit `heavyq stance enter`, which unmutes it.
	Muted  bool    `json:"muted,omitempty"`
	Active *Active `json:"active,omitempty"`
	// Session is the session Counts belong to; a new session starts at zero.
	Session string         `json:"session,omitempty"`
	Counts  map[string]int `json:"counts,omitempty"` // stance -> activations this session
	// Last is when each stance was last entered or left, for the cooldown.
	Last map[string]time.Time `json:"last,omitempty"`
}

const stateSchema = 1

// Event is one line of the stance events log: what happened to which lane and
// why. The log is the measurement's input (stats) and the audit trail.
type Event struct {
	At      time.Time `json:"at"`
	Op      string    `json:"op"` // enter | exit | off
	Lane    string    `json:"lane"`
	Session string    `json:"session,omitempty"`
	Stance  string    `json:"stance,omitempty"`
	Trigger Trigger   `json:"trigger,omitempty"` // enter
	Why     string    `json:"why,omitempty"`     // enter: the evidence
	Item    string    `json:"item,omitempty"`
	Reason  string    `json:"reason,omitempty"` // exit: one of the Exit* reasons
}

// Ops of an Event.
const (
	OpEnter = "enter"
	OpExit  = "exit"
	OpOff   = "off"
)

// Package flowview is a minimal, read-only view of the materialised flow state
// (<git common dir>/heavyq/flow/state.json, schema 1) that the status line, the
// recap and the notifier read. It reads the file directly and never computes
// flow: that is `heavyq flow refresh`'s job.
//
// TODO(#102): internal/flow owns the writer and the full type. When it lands,
// swap State for flow.State and keep only the Forecast adapter and the
// file-system repository finder here (they exist to keep the status line free
// of git and of the flow engine).
package flowview

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SchemaVersion is the only state.json schema this reader understands.
const SchemaVersion = 1

// maxStateBytes bounds what is read: a state file is a few KB.
const maxStateBytes = 1 << 20

// ErrNoState: there is no state file (nothing has been materialised here).
var ErrNoState = errors.New("no flow state")

// State is state.json, schema 1.
type State struct {
	Schema     int         `json:"schema"`
	Repo       string      `json:"repo"`
	Updated    time.Time   `json:"updated"`
	Goal       string      `json:"goal"`
	Run        RawRun      `json:"run"` // null until a run is declared (#103); see RunInfo
	Items      Items       `json:"items"`
	NeedsYou   []NeedsYou  `json:"needs_you"`
	Constraint *Constraint `json:"constraint"`
	Throughput *Throughput `json:"throughput"`
	Lanes      []Lane      `json:"lanes"`
	Session    *Session    `json:"session"`
}

// Items are the counts of work items by state.
type Items struct {
	Total           int `json:"total"`
	Proven          int `json:"proven"`
	Running         int `json:"running"`
	Blocked         int `json:"blocked"`
	ClaimedUnproven int `json:"claimed_unproven"`
	Open            int `json:"open"`
}

// NeedsYou is something only a human can supply.
type NeedsYou struct {
	Item    string    `json:"item"`
	Lane    string    `json:"lane"`
	Session string    `json:"session"`
	Reason  string    `json:"reason"`
	Since   time.Time `json:"since"`
}

// Constraint is what limits the flow right now.
type Constraint struct {
	State       string  `json:"state"`
	Share       float64 `json:"share"`
	Significant bool    `json:"significant"`
}

// Throughput is the measured claim-to-proof pace over a window.
type Throughput struct {
	ProvenPerHour float64 `json:"proven_per_hour"`
	P50ClaimToS   float64 `json:"p50_claim_to_proof_s"`
	WindowS       float64 `json:"window_s"`
	N             int     `json:"n"`
	// LastProof is optional: when the latest proof landed. Without it the
	// forecast cannot tell a stall, only a late ETA.
	LastProof time.Time `json:"last_proof"`
}

// Lane is one lane's current work.
type Lane struct {
	Lane  string    `json:"lane"`
	State string    `json:"state"`
	Item  string    `json:"item"`
	Since time.Time `json:"since"`
}

// Session is what the status line last saw of the agent session.
type Session struct {
	Model        string `json:"model"`
	AgentVersion string `json:"agent_version"`
	RateLimits   struct {
		FiveHourPct int `json:"five_hour_pct"`
		SevenDayPct int `json:"seven_day_pct"`
	} `json:"rate_limits"`
}

// RawRun is the "run" object, kept raw because #103 owns its shape and a field
// of another type must not make the whole state unreadable.
type RawRun = json.RawMessage

// Run is a declared run (preflight): its id, start and promised end.
type Run struct {
	ID          string    `json:"id"`
	Started     time.Time `json:"started"`
	ExpectedEnd time.Time `json:"expected_end"`
	Goal        string    `json:"goal"`
}

// RunInfo decodes the run object; nil when there is none or it does not parse.
func (s *State) RunInfo() *Run {
	if len(s.Run) == 0 || string(s.Run) == "null" {
		return nil
	}
	var r Run
	if json.Unmarshal(s.Run, &r) != nil || r.ID == "" && r.Started.IsZero() && r.ExpectedEnd.IsZero() {
		return nil
	}
	return &r
}

// Path is where the state of the repository with this git common dir lives.
func Path(commonDir string) string {
	return filepath.Join(commonDir, "heavyq", "flow", "state.json")
}

// Read parses the state file. A missing file is ErrNoState; a file of another
// schema, or one that does not parse, is an error: callers say nothing then.
func Read(path string) (*State, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoState
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	if st, err := f.Stat(); err != nil || st.Size() > maxStateBytes {
		return nil, fmt.Errorf("flow state %s: unreadable or too large", path)
	}
	var s State
	if err := json.NewDecoder(f).Decode(&s); err != nil {
		return nil, fmt.Errorf("flow state %s: %w", path, err)
	}
	if s.Schema != SchemaVersion {
		return nil, fmt.Errorf("flow state %s: schema %d, this heavyq reads %d", path, s.Schema, SchemaVersion)
	}
	return &s, nil
}

// Empty reports a state with nothing to show: no items and nothing that needs
// a person.
func (s *State) Empty() bool { return s.Items.Total == 0 && len(s.NeedsYou) == 0 }

// NeedCount is how many things wait for a person: the listed needs, or the
// blocked items when the list is shorter.
func (s *State) NeedCount() int { return max(len(s.NeedsYou), s.Items.Blocked) }

// AllProven: there are items and every one is proven.
func (s *State) AllProven() bool { return s.Items.Total > 0 && s.Items.Proven >= s.Items.Total }

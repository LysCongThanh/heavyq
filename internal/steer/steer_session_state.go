package steer

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// sessionIDRe is what a session id may look like on disk: the same alphabet
// the ledger accepts for session tags, so it is safe as a file name.
var sessionIDRe = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,80}$`)

// ValidSessionID reports whether id can name a session record.
func ValidSessionID(id string) bool { return sessionIDRe.MatchString(id) }

const (
	sessionStateVersion = 1
	maxSessionFileBytes = 64 << 10
	maxPrunePerStart    = 200
)

// SessionState is what heavyq remembers about one agent session, in one small
// file under the git common dir. It is a record, not a contract: a missing or
// damaged file means "no memory", and every decision then fails open.
type SessionState struct {
	V       int       `json:"v"`
	Started time.Time `json:"started"`
	// HasBaseline says the session recorded what was red when it started.
	// Without it a red item is never blocked on (nobody can say whose fault it is).
	HasBaseline bool `json:"has_baseline"`
	// Baseline is the proof commands that already failed at SessionStart:
	// "pre-existing", never a reason to block this session.
	Baseline []string `json:"baseline,omitempty"`
	// Blocks counts the Stop hook blocks so far (the guard cap).
	Blocks int `json:"blocks"`
	// LastSig identifies the situation of the latest block: the same signature
	// with stop_hook_active set means the agent made no progress.
	LastSig string `json:"last_sig,omitempty"`
	// ClaimNudged: the one claim-vs-ledger continuation was used.
	ClaimNudged bool `json:"claim_nudged,omitempty"`
	// Canary is the token the latest brief carried, Transcript the transcript
	// path the hook was given: `heavyq doctor` looks for one in the other.
	Canary     string    `json:"canary,omitempty"`
	CanaryAt   time.Time `json:"canary_at,omitzero"`
	Transcript string    `json:"transcript,omitempty"`
}

// IsBaseline reports whether cmd was already failing when the session began.
func (s SessionState) IsBaseline(cmd string) bool {
	if !s.HasBaseline {
		return false
	}
	c := clipCmd(cmd)
	for _, b := range s.Baseline {
		if b == c {
			return true
		}
	}
	return false
}

// Sessions is the directory of session records of one repository.
type Sessions struct{ dir string }

// OpenSessions names the store under commonDir. Nothing is created until Save.
func OpenSessions(commonDir string) Sessions {
	return Sessions{dir: filepath.Join(commonDir, "heavyq", "steer-sessions")}
}

func (s Sessions) path(id string) string { return filepath.Join(s.dir, id+".json") }

// Load reads the record of session id. ok is false when there is none, it is
// damaged, oversized or from another schema.
func (s Sessions) Load(id string) (st SessionState, ok bool) {
	if !ValidSessionID(id) {
		return SessionState{}, false
	}
	f, err := os.Open(s.path(id))
	if err != nil {
		return SessionState{}, false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxSessionFileBytes+1))
	if err != nil || len(b) > maxSessionFileBytes {
		return SessionState{}, false
	}
	if json.Unmarshal(b, &st) != nil || st.V != sessionStateVersion {
		return SessionState{}, false
	}
	return st, true
}

// Save replaces the record of session id atomically (mode 0600).
func (s Sessions) Save(id string, st SessionState) error {
	if !ValidSessionID(id) {
		return fmt.Errorf("session id %q is not a valid tag", id)
	}
	st.V = sessionStateVersion
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	return writeFileAtomic(s.path(id), append(b, '\n'))
}

// Prune removes records not touched for maxAge, at most maxPrunePerStart per
// call, so SessionStart stays cheap. Errors are dropped: pruning is hygiene.
func (s Sessions) Prune(maxAge time.Duration, now time.Time) {
	ents, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	n := 0
	for _, e := range ents {
		if n >= maxPrunePerStart {
			return
		}
		n++
		fi, err := e.Info()
		if err == nil && fi.Mode().IsRegular() && now.Sub(fi.ModTime()) > maxAge {
			_ = os.Remove(filepath.Join(s.dir, e.Name()))
		}
	}
}

// LatestCanary returns the session record with a canary that was written last,
// and its id. ok is false when no session ever recorded one.
func (s Sessions) LatestCanary() (id string, st SessionState, ok bool) {
	ents, err := os.ReadDir(s.dir)
	if err != nil {
		return "", SessionState{}, false
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].Name() < ents[j].Name() })
	for _, e := range ents {
		name := e.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		cand := name[:len(name)-len(".json")]
		got, ok2 := s.Load(cand)
		if !ok2 || got.Canary == "" {
			continue
		}
		if !ok || got.CanaryAt.After(st.CanaryAt) {
			id, st, ok = cand, got, true
		}
	}
	return id, st, ok
}

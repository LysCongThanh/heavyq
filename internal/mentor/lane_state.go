package mentor

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/LysCongThanh/heavyq/internal/queue"
)

// Limits bound how often a rule may speak.
type Limits struct {
	// Cap is the most notes one rule may raise per session and lane (design
	// rule 3). Held-out events count too, so both arms see the same events.
	Cap int
	// Cooldown is the least time between two events of one signature.
	Cooldown time.Duration
}

// DefaultLimits are the limits of the research design: at most 3 per rule and
// session, and 3 seconds between two events of one signature.
var DefaultLimits = Limits{Cap: 3, Cooldown: 3 * time.Second}

const (
	laneStateVersion = 1
	maxSignatures    = 32
	maxPending       = 16
	maxLaneStateSize = 64 << 10
)

// SigState is what is remembered of one signature: the highest level raised
// and when.
type SigState struct {
	Level int       `json:"level"`
	At    time.Time `json:"at"`
}

// Pending is an event whose outcome is not known yet. It is kept so the next
// hook call on the lane can resolve it while its records are still indexed.
type Pending struct {
	Entry Entry `json:"entry"`
}

// LaneState is what the mentor remembers of one lane between hook calls:
// which ledger record it looked at last, how many times each rule spoke in
// this session, the levels raised per signature, and events awaiting an
// outcome. It lives in the queue directory (runtime state, mode 0600): losing
// it only means a rule may speak again after a reboot.
type LaneState struct {
	Version    int                 `json:"v"`
	Session    string              `json:"session,omitempty"`
	LastRecord string              `json:"last_record,omitempty"`
	Fires      map[string]int      `json:"fires,omitempty"`
	Sigs       map[string]SigState `json:"sigs,omitempty"`
	Pending    []Pending           `json:"pending,omitempty"`
}

func newLaneState() *LaneState {
	return &LaneState{Version: laneStateVersion, Fires: map[string]int{}, Sigs: map[string]SigState{}}
}

// Admit decides whether signal s may be raised in session, and records it when
// it may. Four gates, all of which must pass:
//   - a new session starts from nothing (counts and signatures reset);
//   - state change only: s must reach a higher level than the one already
//     raised for its signature, so a repeated state is never repeated aloud;
//   - cooldown: a signature that already spoke waits Limits.Cooldown, measured
//     on the ledger's clock (the records' End), not the wall clock;
//   - cap: at most Limits.Cap events per rule and session.
func (st *LaneState) Admit(s Signal, session string, l Limits) bool {
	if st.Session != session {
		st.Session, st.Fires, st.Sigs = session, map[string]int{}, map[string]SigState{}
	}
	key := s.Rule + "|" + s.Signature
	prev := st.Sigs[key]
	switch {
	case s.Level <= prev.Level:
		return false
	case prev.Level > 0 && s.At.Sub(prev.At) < l.Cooldown:
		return false
	case st.Fires[s.Rule] >= l.Cap:
		return false
	}
	st.Sigs[key] = SigState{Level: s.Level, At: s.At}
	st.Fires[s.Rule]++
	st.trim()
	return true
}

// trim keeps the signature map and the pending list bounded: the oldest go first.
func (st *LaneState) trim() {
	if len(st.Sigs) > maxSignatures {
		keys := make([]string, 0, len(st.Sigs))
		for k := range st.Sigs {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return st.Sigs[keys[i]].At.Before(st.Sigs[keys[j]].At) })
		for _, k := range keys[:len(keys)-maxSignatures] {
			delete(st.Sigs, k)
		}
	}
	if len(st.Pending) > maxPending {
		st.Pending = append([]Pending(nil), st.Pending[len(st.Pending)-maxPending:]...)
	}
}

func (s *Store) lanePath(lane string) string {
	return filepath.Join(s.LaneDir, queue.SanitizeLane(lane)+".json")
}

// LoadLane reads the state of lane. A missing, oversized or unreadable file is
// an empty state: the mentor starts over rather than guessing.
func (s *Store) LoadLane(lane string) *LaneState {
	path := s.lanePath(lane)
	if fi, err := os.Lstat(path); err != nil || !fi.Mode().IsRegular() || fi.Size() > maxLaneStateSize {
		return newLaneState()
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return newLaneState()
	}
	st := newLaneState()
	if json.Unmarshal(b, st) != nil || st.Version != laneStateVersion {
		return newLaneState()
	}
	if st.Fires == nil {
		st.Fires = map[string]int{}
	}
	if st.Sigs == nil {
		st.Sigs = map[string]SigState{}
	}
	return st
}

// SaveLane writes the state of lane atomically (temp file and rename, 0600).
func (s *Store) SaveLane(lane string, st *LaneState) error {
	if err := os.MkdirAll(s.LaneDir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return writeFileAtomic(s.lanePath(lane), b)
}

// writeFileAtomic replaces path with data: a reader sees the old file or the
// new one, never half of one.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(name, 0o600)
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}

// readFileIfExists is os.ReadFile, with a missing file reported as nil data.
func readFileIfExists(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/slots"
)

// Flight coordinates identical runs that are in progress at the same time.
// It lives in the queue directory (runtime state, like tickets):
//
//	<key>.lock          flock held by the leader for its whole run
//	<key>.leader        who leads (for status)
//	<key>.<pid>.wait    one per follower waiting for the leader's result
type Flight struct{ Dir string }

// Party is a leader or follower as shown by status.
type Party struct {
	Key   string    `json:"key"`
	Pid   int       `json:"pid"`
	Start string    `json:"start"` // process start token (pid reuse guard)
	Lane  string    `json:"lane"`
	Cmd   string    `json:"cmd"`
	Since time.Time `json:"since"`
}

// TryLead takes leadership of key. It returns nil (and no error) when
// another process leads it right now.
func (f *Flight) TryLead(p Party) (*slots.Lock, error) {
	if !keyRe.MatchString(p.Key) {
		return nil, fmt.Errorf("invalid cache key")
	}
	l, err := slots.TryLock(filepath.Join(f.Dir, p.Key+".lock"))
	if err != nil || l == nil {
		return nil, err
	}
	if err := f.write(p.Key+".leader", p); err != nil {
		l.Release()
		return nil, err
	}
	return l, nil
}

// EndLead removes the leader marker and releases the lock.
func (f *Flight) EndLead(key string, l *slots.Lock) {
	_ = os.Remove(filepath.Join(f.Dir, key+".leader"))
	l.Release()
}

// Follow registers a waiting follower; the returned func unregisters it.
func (f *Flight) Follow(p Party) (func(), error) {
	name := fmt.Sprintf("%s.%d.wait", p.Key, p.Pid)
	if err := f.write(name, p); err != nil {
		return func() {}, err
	}
	return func() { _ = os.Remove(filepath.Join(f.Dir, name)) }, nil
}

// Leader returns the current leader of key, if its marker is readable.
func (f *Flight) Leader(key string) (Party, bool) {
	var p Party
	b, err := os.ReadFile(filepath.Join(f.Dir, key+".leader"))
	return p, err == nil && json.Unmarshal(b, &p) == nil
}

// Group is a leader with the followers waiting on it.
type Group struct {
	Leader    Party
	HasLeader bool
	Followers []Party
}

// Groups lists in-flight groups that have followers. Markers of dead
// processes are removed.
func (f *Flight) Groups(alive func(pid int, start string) bool) []Group {
	ents, err := os.ReadDir(f.Dir)
	if err != nil {
		return nil
	}
	by := map[string]*Group{}
	for _, e := range ents {
		n := e.Name()
		isWait, isLead := strings.HasSuffix(n, ".wait"), strings.HasSuffix(n, ".leader")
		if !isWait && !isLead {
			continue
		}
		var p Party
		b, err := os.ReadFile(filepath.Join(f.Dir, n))
		if err != nil || json.Unmarshal(b, &p) != nil || !keyRe.MatchString(p.Key) {
			continue
		}
		if !alive(p.Pid, p.Start) {
			_ = os.Remove(filepath.Join(f.Dir, n))
			continue
		}
		g := by[p.Key]
		if g == nil {
			g = &Group{}
			by[p.Key] = g
		}
		if isLead {
			g.Leader, g.HasLeader = p, true
		} else {
			g.Followers = append(g.Followers, p)
		}
	}
	var out []Group
	for _, g := range by {
		if len(g.Followers) > 0 {
			sort.Slice(g.Followers, func(i, j int) bool { return g.Followers[i].Since.Before(g.Followers[j].Since) })
			out = append(out, *g)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Followers[0].Since.Before(out[j].Followers[0].Since) })
	return out
}

func (f *Flight) write(name string, p Party) error {
	if err := os.MkdirAll(f.Dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tmp := filepath.Join(f.Dir, "."+name+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(f.Dir, name)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

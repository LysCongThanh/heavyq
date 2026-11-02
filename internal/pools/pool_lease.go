package pools

import (
	"fmt"
	"strconv"

	"github.com/LysCongThanh/heavyq/internal/slots"
)

// Lease is the set of values one run holds. Each is a flock held by this
// process; Release gives them back, and so does the end of the process.
type Lease struct {
	needs  []Need
	values map[string][]string
	locks  []*slots.Lock
}

// Values maps each pool to the values held, in the order they are exported.
func (l *Lease) Values() map[string][]string {
	if l == nil {
		return nil
	}
	return l.values
}

// Env is the environment that tells the command what it holds: HEAVYQ_PG for
// one value of pg, HEAVYQ_PORT_1 and HEAVYQ_PORT_2 for two of port.
func (l *Lease) Env() []string {
	if l == nil {
		return nil
	}
	var out []string
	for _, n := range l.needs {
		for i, name := range n.EnvNames() {
			out = append(out, name+"="+l.values[n.Pool][i])
		}
	}
	return out
}

// Release unlocks every value. Safe on nil and when repeated.
func (l *Lease) Release() {
	if l == nil {
		return
	}
	for _, k := range l.locks {
		k.Release()
	}
	l.locks, l.values = nil, nil
}

// Shortfall says which pool could not give what was asked, and how much of it
// was free at that moment.
type Shortfall struct {
	Pool     string
	Need     int
	Free     int
	Capacity int
}

// Acquire takes every need or nothing: when one pool cannot give its values,
// everything taken so far is released and the Shortfall says why. Call it
// under the queue mutex, so two runs never take parts of each other's needs.
// A pool the set does not know is an error (Validate catches it earlier).
func (s *Set) Acquire(needs []Need) (*Lease, *Shortfall, error) {
	for _, n := range needs {
		if _, ok := s.pools[n.Pool]; !ok {
			return nil, nil, fmt.Errorf("unknown pool %q", n.Pool)
		}
	}
	// Look before taking: a shortfall visible without locking returns at once,
	// so a waiting ticket never holds (or probes) values of other pools round
	// after round. Only a race between this check and the take below can still
	// make Acquire take and release, and the queue mutex makes that rare.
	if sf := s.Check(needs); sf != nil {
		return nil, sf, nil
	}
	l := &Lease{needs: needs, values: map[string][]string{}}
	for _, n := range needs {
		p, ok := s.pools[n.Pool]
		if !ok {
			l.Release()
			return nil, nil, fmt.Errorf("unknown pool %q", n.Pool)
		}
		vals := s.Values(p)
		for _, v := range vals {
			if len(l.values[n.Pool]) == n.N {
				break
			}
			lock, err := slots.TryLock(s.lockPath(n.Pool, v))
			if err != nil {
				l.Release()
				return nil, nil, err
			}
			if lock == nil {
				continue // held by another run
			}
			if p.Port && s.Bound(portOf(v)) {
				lock.Release() // something outside heavyq listens on it
				continue
			}
			l.locks = append(l.locks, lock)
			l.values[n.Pool] = append(l.values[n.Pool], v)
		}
		if got := len(l.values[n.Pool]); got < n.N {
			l.Release()
			// Every value was tried, so got is exactly what was free.
			return nil, &Shortfall{Pool: n.Pool, Need: n.N, Free: got, Capacity: len(vals)}, nil
		}
	}
	return l, nil, nil
}

// Check is a dry Acquire for display (status, why): the first Shortfall, or
// nil when every need could be met now. It keeps nothing, and a pool it does
// not know is skipped, since a ticket of another repository may use it.
func (s *Set) Check(needs []Need) *Shortfall {
	for _, n := range needs {
		p, ok := s.pools[n.Pool]
		if !ok {
			continue
		}
		vals := s.Values(p)
		free := 0
		for _, v := range vals {
			if free == n.N {
				break
			}
			if slots.Busy(s.lockPath(n.Pool, v)) || (p.Port && s.Bound(portOf(v))) {
				continue
			}
			free++
		}
		if free < n.N {
			return &Shortfall{Pool: n.Pool, Need: n.N, Free: free, Capacity: len(vals)}
		}
	}
	return nil
}

func portOf(v string) int {
	n, _ := strconv.Atoi(v) // a port pool's values were checked to be numbers
	return n
}

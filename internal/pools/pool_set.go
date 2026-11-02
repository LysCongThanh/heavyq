package pools

import (
	"fmt"
	"path/filepath"
	"strconv"
)

// Set is the pools a run can ask for, and where their lock files live.
type Set struct {
	// Dir holds one directory per pool with one "<value>.lock" file per value
	// (<queue dir>/pools).
	Dir string
	// Eph is the OS ephemeral port range: port pools never hand out a value
	// inside it.
	Eph PortRange
	// Bound probes a port; PortBound unless a test replaces it.
	Bound func(port int) bool

	order []string
	pools map[string]Pool
}

// NewSet returns the set of ps, with lock files under dir. A later pool of
// the same name replaces an earlier one.
func NewSet(dir string, ps []Pool) *Set {
	s := &Set{Dir: dir, Eph: LocalEphemeralRange(), Bound: PortBound, pools: map[string]Pool{}}
	for _, p := range ps {
		if _, dup := s.pools[p.Name]; !dup {
			s.order = append(s.order, p.Name)
		}
		s.pools[p.Name] = p
	}
	return s
}

// Names lists the pools in definition order.
func (s *Set) Names() []string { return append([]string(nil), s.order...) }

// Pool returns the pool called name.
func (s *Set) Pool(name string) (Pool, bool) {
	p, ok := s.pools[name]
	return p, ok
}

// Values are the values of p a run can get: all of them, except that a port
// pool leaves out ports inside the OS ephemeral range.
func (s *Set) Values(p Pool) []string {
	if !p.Port {
		return p.Values
	}
	out := make([]string, 0, len(p.Values))
	for _, v := range p.Values {
		if n, err := strconv.Atoi(v); err == nil && !s.Eph.Contains(n) {
			out = append(out, v)
		}
	}
	return out
}

// Capacity is how many values of the pool a run can ever get (0: unknown pool).
func (s *Set) Capacity(name string) int {
	p, ok := s.pools[name]
	if !ok {
		return 0
	}
	return len(s.Values(p))
}

// Validate refuses needs that can never be met: an unknown pool, or more
// values than the pool has once the ephemeral range is left out. Waiting for
// them would never end.
func (s *Set) Validate(needs []Need) error {
	for _, n := range needs {
		p, ok := s.pools[n.Pool]
		if !ok {
			return fmt.Errorf("unknown pool %q (define it with [[pool]] in the config or in heavyq.toml)", n.Pool)
		}
		have := s.Capacity(n.Pool)
		if n.N <= have {
			continue
		}
		if p.Port && len(p.Values) >= n.N {
			return fmt.Errorf("pool %q has %d ports outside the OS ephemeral range %d-%d; %d needed", n.Pool, have, s.Eph.First, s.Eph.Last, n.N)
		}
		return fmt.Errorf("pool %q has %d values; %d needed", n.Pool, have, n.N)
	}
	return nil
}

func (s *Set) lockPath(pool, value string) string {
	return filepath.Join(s.Dir, pool, value+".lock")
}

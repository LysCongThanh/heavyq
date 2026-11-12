// Package pools hands out named resources (ports, database names, browser
// seats) to parallel runs.
//
// A pool is a set of values. A run asks for some of them with --needs and
// holds them as long as it runs, exactly as it holds a slot: one flock(2) file
// per value, taken by the heavyq process of the run, so a value is released
// when that process ends, however it ends. The scheduler takes the slot and
// every need in one round under the queue mutex, and gives all of them back
// when one cannot be had, so a ticket never holds part of what it asked for.
package pools

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Spec is one [[pool]] table as written in the user config or heavyq.toml.
type Spec struct {
	Name string `toml:"name"`
	// Values is a range string "4100-4199" or a list of strings / integers.
	// Absent for a capacity-only pool.
	Values any `toml:"values"`
	// Size makes a capacity-only pool: the values are 1..Size.
	Size int `toml:"size"`
	// Port marks a pool of TCP ports, which are probed before they are handed
	// out. Absent: a pool called "port" or ending in "_port" is a port pool.
	Port *bool `toml:"port"`
}

// Pool is a validated pool.
type Pool struct {
	Name   string
	Values []string // in hand-out order
	Port   bool     // values are TCP ports: skip bound ones and the ephemeral range
}

// Limits.
const (
	MaxValues = 65535 // values per pool (a whole port space)
	MaxSize   = 1024  // capacity of a capacity-only pool
	MaxNeed   = 64    // values one run may ask of one pool
)

var (
	// poolName is the shape of a pool name: it becomes part of an environment
	// variable (HEAVYQ_<NAME>) and of a directory name.
	poolName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	// valueName is the shape of a value: it becomes a file name and the content
	// of an environment variable.
	valueName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	rangeSpec = regexp.MustCompile(`^\s*(\d+)\s*-\s*(\d+)\s*$`)
)

// ValidName reports whether s is an acceptable pool name: the right shape and
// not reserved (pool_reserved_names.go).
func ValidName(s string) bool { return poolName.MatchString(s) && !reserved[s] }

// Build validates specs and returns the pools in file order. Two pools of one
// name in one file are an error.
func Build(specs []Spec) ([]Pool, error) {
	out := make([]Pool, 0, len(specs))
	seen := map[string]bool{}
	for i, sp := range specs {
		p, err := sp.build()
		if err != nil {
			return nil, fmt.Errorf("[[pool]] #%d%s: %w", i+1, nameHint(sp.Name), err)
		}
		if seen[p.Name] {
			return nil, fmt.Errorf("[[pool]] #%d: duplicate pool name %q", i+1, p.Name)
		}
		seen[p.Name] = true
		out = append(out, p)
	}
	return out, nil
}

func nameHint(name string) string {
	if name == "" {
		return ""
	}
	return fmt.Sprintf(" (%q)", name)
}

func (sp Spec) build() (Pool, error) {
	if !poolName.MatchString(sp.Name) {
		return Pool{}, fmt.Errorf("name %q must match %s", sp.Name, poolName)
	}
	if reserved[sp.Name] {
		return Pool{}, fmt.Errorf("name %q is reserved (heavyq sets or reads HEAVYQ_%s)", sp.Name, strings.ToUpper(sp.Name))
	}
	p := Pool{Name: sp.Name, Port: sp.Name == "port" || strings.HasSuffix(sp.Name, "_port")}
	if sp.Port != nil {
		p.Port = *sp.Port
	}
	switch {
	case sp.Values != nil && sp.Size != 0:
		return Pool{}, fmt.Errorf("set values or size, not both")
	case sp.Values != nil:
		vals, err := parseValues(sp.Values)
		if err != nil {
			return Pool{}, err
		}
		p.Values = vals
	case sp.Size < 0 || sp.Size > MaxSize:
		return Pool{}, fmt.Errorf("size %d must be between 1 and %d", sp.Size, MaxSize)
	case sp.Size > 0:
		for i := 1; i <= sp.Size; i++ {
			p.Values = append(p.Values, strconv.Itoa(i))
		}
	default:
		return Pool{}, fmt.Errorf("set values (a range like \"4100-4199\" or a list) or size")
	}
	if p.Port {
		for _, v := range p.Values {
			if n, err := strconv.Atoi(v); err != nil || n < 1 || n > 65535 {
				return Pool{}, fmt.Errorf("a port pool holds TCP ports 1-65535, not %q (port = false makes it a plain pool)", v)
			}
		}
	}
	return p, nil
}

// Merge returns base with the pools of over added; a pool of the same name in
// over replaces the one in base (the repository wins over the user config).
func Merge(base, over []Pool) []Pool {
	out := make([]Pool, 0, len(base)+len(over))
	replaced := map[string]bool{}
	for _, p := range over {
		replaced[p.Name] = true
	}
	for _, p := range base {
		if !replaced[p.Name] {
			out = append(out, p)
		}
	}
	return append(out, over...)
}

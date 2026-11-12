package pools

import (
	"fmt"
	"strconv"
	"strings"
)

// Need is what a run asks of one pool: N values of it.
type Need struct {
	Pool string
	N    int
}

// String is the spec form: "pg" for one value, "port:2" for more.
func (n Need) String() string {
	if n.N == 1 {
		return n.Pool
	}
	return n.Pool + ":" + strconv.Itoa(n.N)
}

// EnvNames are the variables that carry the values of the need: HEAVYQ_PG
// for one, HEAVYQ_PORT_1 .. HEAVYQ_PORT_N for several.
func (n Need) EnvNames() []string {
	base := "HEAVYQ_" + strings.ToUpper(n.Pool)
	if n.N == 1 {
		return []string{base}
	}
	out := make([]string, n.N)
	for i := range out {
		out[i] = base + "_" + strconv.Itoa(i+1)
	}
	return out
}

// ParseNeeds reads "pool[:n],pool[:n]...". Each pool may appear once, and no
// two needs may export the same variable (port:2 exports HEAVYQ_PORT_1 and
// HEAVYQ_PORT_2, which a pool called port_1 would also claim).
func ParseNeeds(spec string) ([]Need, error) {
	var out []Need
	seen := map[string]bool{}
	envs := map[string]string{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("needs %q has an empty entry", spec)
		}
		name, count, hasN := strings.Cut(part, ":")
		n := 1
		if hasN {
			var err error
			if n, err = strconv.Atoi(count); err != nil || n < 1 || n > MaxNeed {
				return nil, fmt.Errorf("needs %q: the count after %q must be 1-%d", part, name+":", MaxNeed)
			}
		}
		if reserved[name] {
			return nil, fmt.Errorf("needs %q: pool name %q is reserved (heavyq sets or reads HEAVYQ_%s)", part, name, strings.ToUpper(name))
		}
		if !ValidName(name) {
			return nil, fmt.Errorf("needs %q: %q is not a pool name (%s)", part, name, poolName)
		}
		if seen[name] {
			return nil, fmt.Errorf("needs names pool %q twice; ask for the total once (%s:N)", name, name)
		}
		seen[name] = true
		need := Need{Pool: name, N: n}
		for _, v := range need.EnvNames() {
			if other, dup := envs[v]; dup {
				return nil, fmt.Errorf("needs: %s would be set by both %q and %q", v, other, name)
			}
			envs[v] = name
		}
		out = append(out, need)
	}
	return out, nil
}

// ParseNeedList reads a list of specs (a gate's needs = [...], a ticket's
// recorded needs) as ParseNeeds reads one comma-separated string.
func ParseNeedList(specs []string) ([]Need, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	for _, s := range specs {
		if strings.Contains(s, ",") {
			return nil, fmt.Errorf("needs entry %q holds several needs; list them separately", s)
		}
	}
	return ParseNeeds(strings.Join(specs, ","))
}

// FormatNeeds is the spec form of each need, for tickets and the ledger.
func FormatNeeds(needs []Need) []string {
	out := make([]string, len(needs))
	for i, n := range needs {
		out[i] = n.String()
	}
	return out
}

// FormatHeld writes the values a run held for the ledger: "port=4100,4101
// pg=app_wt1", pools in the order of needs. "" when nothing is held.
func FormatHeld(needs []Need, held map[string][]string) string {
	var parts []string
	for _, n := range needs {
		if v := held[n.Pool]; len(v) > 0 {
			parts = append(parts, n.Pool+"="+strings.Join(v, ","))
		}
	}
	return strings.Join(parts, " ")
}

package pools

import (
	"fmt"
	"strconv"
)

// parseValues reads the values key: a range string or a list.
func parseValues(v any) ([]string, error) {
	switch x := v.(type) {
	case string:
		return ParseRange(x)
	case []any:
		if len(x) == 0 {
			return nil, fmt.Errorf("values is an empty list")
		}
		if len(x) > MaxValues {
			return nil, fmt.Errorf("values has %d entries; the limit is %d", len(x), MaxValues)
		}
		seen := map[string]bool{}
		out := make([]string, 0, len(x))
		for _, e := range x {
			var s string
			switch n := e.(type) {
			case string:
				s = n
			case int64:
				s = strconv.FormatInt(n, 10)
			default:
				return nil, fmt.Errorf("values entries must be strings or integers, got %v", e)
			}
			if !valueName.MatchString(s) {
				return nil, fmt.Errorf("value %q must match %s", s, valueName)
			}
			if seen[s] {
				return nil, fmt.Errorf("value %q is listed twice", s)
			}
			seen[s] = true
			out = append(out, s)
		}
		return out, nil
	}
	return nil, fmt.Errorf("values must be a range string like \"4100-4199\" or a list, got %v", v)
}

// ParseRange expands "a-b" (inclusive, a <= b) into its integers.
func ParseRange(s string) ([]string, error) {
	m := rangeSpec.FindStringSubmatch(s)
	if m == nil {
		return nil, fmt.Errorf("values %q is not a range like \"4100-4199\" (use a list for names)", s)
	}
	lo, err1 := strconv.Atoi(m[1])
	hi, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil || lo > hi {
		return nil, fmt.Errorf("range %q must run from a smaller to a larger number", s)
	}
	if hi-lo+1 > MaxValues {
		return nil, fmt.Errorf("range %q has more than %d values", s, MaxValues)
	}
	out := make([]string, 0, hi-lo+1)
	for i := lo; i <= hi; i++ {
		out = append(out, strconv.Itoa(i))
	}
	return out, nil
}

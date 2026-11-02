package preflight

import (
	"fmt"
	"strings"
)

// Disk thresholds on the repository's file system.
const (
	DiskBlockBytes = 1 << 30 // under 1 GiB a build or a cache write will fail
	DiskWarnBytes  = 5 << 30
)

// MachineFact is one machine of the queue as preflight found it.
type MachineFact struct {
	Name       string
	Local      bool
	SSH        string // the ssh alias of a remote, for the fix
	Reachable  bool   // a remote answered the probe within the timeout (always true for local)
	Err        string // why a remote did not
	HeavySlots int    // configured heavy slots
}

// PoolFact is a named pool and how many values it holds.
type PoolFact struct {
	Name     string
	Capacity int
}

// EnvInput is what CheckEnvironment reads.
type EnvInput struct {
	Parallel      int
	DiskPath      string
	DiskKnown     bool
	DiskFreeBytes uint64
	Machines      []MachineFact
	Pools         []PoolFact
}

// CheckEnvironment: disk space, remotes that answer, and slot and pool
// capacity against the planned parallelism.
func CheckEnvironment(in EnvInput) []Finding {
	var out []Finding
	if in.DiskKnown {
		switch {
		case in.DiskFreeBytes < DiskBlockBytes:
			out = append(out, Finding{Severity: Block, Subject: in.DiskPath,
				Text: fmt.Sprintf("only %s free on the file system of %s: builds and caches will fail mid-run", gib(in.DiskFreeBytes), in.DiskPath),
				Fix:  "heavyq cache clear"})
		case in.DiskFreeBytes < DiskWarnBytes:
			out = append(out, Finding{Severity: Warn, Subject: in.DiskPath,
				Text: fmt.Sprintf("%s free on the file system of %s: a long run may fill it", gib(in.DiskFreeBytes), in.DiskPath),
				Fix:  "heavyq cache clear"})
		}
	}
	slots, parts := 0, []string{}
	for _, m := range in.Machines {
		if !m.Reachable {
			fix := "heavyq status"
			if m.SSH != "" {
				fix = "ssh " + m.SSH + " true"
			}
			out = append(out, Finding{Severity: Warn, Subject: m.Name,
				Text: fmt.Sprintf("remote %s did not answer (%s): runs placed there wait, then give up (exit 76)", m.Name, orUnknown(m.Err)), Fix: fix})
			continue
		}
		slots += m.HeavySlots
		parts = append(parts, fmt.Sprintf("%s %d", m.Name, m.HeavySlots))
	}
	if in.Parallel > 0 && len(in.Machines) > 0 && in.Parallel > slots {
		out = append(out, Finding{Severity: Warn,
			Text: fmt.Sprintf("%d lanes planned but %d heavy slots in all (%s): the rest queue behind them", in.Parallel, slots, strings.Join(parts, ", ")),
			Fix:  fmt.Sprintf("heavyq preflight --parallel %d", max(slots, 1))})
	}
	for _, p := range in.Pools {
		if in.Parallel > 0 && p.Capacity > 0 && p.Capacity < in.Parallel {
			out = append(out, Finding{Severity: Warn, Subject: p.Name,
				Text: fmt.Sprintf("pool %s holds %d values for %d lanes: runs that need it (--needs %s) queue behind each other", p.Name, p.Capacity, in.Parallel, p.Name),
				Fix:  fmt.Sprintf("heavyq preflight --parallel %d", p.Capacity)})
		}
	}
	return out
}

func gib(b uint64) string { return fmt.Sprintf("%.1f GiB", float64(b)/(1<<30)) }

func orUnknown(s string) string {
	if s == "" {
		return "unreachable"
	}
	return s
}

package metrics

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// bytesPerMB is the unit of Metrics.FreeMB and TotalMB (MiB).
const bytesPerMB = 1 << 20

// parseSysctl parses `sysctl -n hw.memsize hw.ncpu vm.loadavg`: one value
// per line, in that order, e.g.
//
//	17179869184
//	8
//	{ 2.34 2.10 1.98 }
//
// It returns total memory in bytes, logical CPUs and the 1-minute load.
func parseSysctl(out string) (memBytes uint64, ncpu int, load1 float64, err error) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 3 {
		return 0, 0, 0, fmt.Errorf("sysctl: want 3 lines, got %d", len(lines))
	}
	if memBytes, err = strconv.ParseUint(strings.TrimSpace(lines[0]), 10, 64); err != nil || memBytes == 0 {
		return 0, 0, 0, fmt.Errorf("sysctl hw.memsize: %q", lines[0])
	}
	if ncpu, err = strconv.Atoi(strings.TrimSpace(lines[1])); err != nil || ncpu < 1 {
		return 0, 0, 0, fmt.Errorf("sysctl hw.ncpu: %q", lines[1])
	}
	// vm.loadavg is "{ 1m 5m 15m }", sometimes followed by a scale factor.
	f := strings.Fields(strings.Trim(strings.TrimSpace(lines[2]), "{}"))
	if len(f) == 0 {
		return 0, 0, 0, fmt.Errorf("sysctl vm.loadavg: %q", lines[2])
	}
	if load1, err = strconv.ParseFloat(f[0], 64); err != nil || load1 < 0 {
		return 0, 0, 0, fmt.Errorf("sysctl vm.loadavg: %q", lines[2])
	}
	return memBytes, ncpu, load1, nil
}

// parseVMStat parses `vm_stat` and returns the bytes of memory the system
// can hand to a new job without paging out working data:
//
//	free + inactive + purgeable + speculative pages, times the page size.
//
// Why this and not "Pages free": macOS keeps every spare page busy as file
// cache, so "free" alone sits near zero on a healthy machine and a gate on
// it would never admit anything. The other three are memory the kernel
// gives back on demand: inactive (not touched recently, first to be
// reclaimed), purgeable (caches apps volunteered to discard) and
// speculative (read-ahead that may never be used). Active, wired and
// compressed pages are excluded: they hold live data. It is an estimate,
// slightly optimistic because some purgeable pages are also counted inactive
// and inactive can hold dirty data that must be written first; the caller
// caps it at total memory.
func parseVMStat(out string) (availBytes uint64, err error) {
	var pageSize uint64
	pages := map[string]uint64{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "Mach Virtual Memory Statistics") {
			pageSize = pageSizeOf(line)
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok || !strings.HasPrefix(key, "Pages ") {
			continue
		}
		if n, perr := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(val), "."), 10, 64); perr == nil {
			pages[key] = n
		}
	}
	if pageSize == 0 {
		return 0, fmt.Errorf("vm_stat: no page size in header")
	}
	for _, need := range []string{"Pages free", "Pages inactive"} {
		if _, ok := pages[need]; !ok {
			return 0, fmt.Errorf("vm_stat: no %q line", need)
		}
	}
	n := pages["Pages free"] + pages["Pages inactive"] + pages["Pages purgeable"] + pages["Pages speculative"]
	return n * pageSize, nil
}

// pageSizeOf extracts N from "...(page size of N bytes)"; 0 when absent.
func pageSizeOf(header string) uint64 {
	_, after, ok := strings.Cut(header, "page size of ")
	if !ok {
		return 0
	}
	num, _, _ := strings.Cut(after, " ")
	n, err := strconv.ParseUint(num, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

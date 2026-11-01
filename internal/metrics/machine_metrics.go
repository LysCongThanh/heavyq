// Package metrics measures machine resources used by the admission gate.
package metrics

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Metrics is one resource sample of a machine.
type Metrics struct {
	FreeMB  int     // MemAvailable in MiB
	TotalMB int     // MemTotal in MiB (0 when unknown)
	Load1   float64 // 1-minute load average
	Cores   int
	// Known is false when the platform cannot be measured (e.g. no /proc);
	// the gate then only enforces slots.
	Known bool
}

// String renders a compact human summary.
func (m Metrics) String() string {
	if !m.Known {
		return "resources unknown on this platform"
	}
	return fmt.Sprintf("%d cores, load %.2f, %d MB available", m.Cores, m.Load1, m.FreeMB)
}

// Local measures the local machine from procRoot (normally "/proc").
func Local(procRoot string) Metrics {
	free, err := readMemMB(filepath.Join(procRoot, "meminfo"), "MemAvailable:")
	if err != nil {
		return Metrics{Cores: runtime.NumCPU()}
	}
	total, _ := readMemMB(filepath.Join(procRoot, "meminfo"), "MemTotal:")
	load, err := readLoad1(filepath.Join(procRoot, "loadavg"))
	if err != nil {
		return Metrics{Cores: runtime.NumCPU()}
	}
	return Metrics{FreeMB: free, TotalMB: total, Load1: load, Cores: runtime.NumCPU(), Known: true}
}

func readMemMB(path, key string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[0] == key {
			kb, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return 0, err
			}
			return int(kb / 1024), nil
		}
	}
	if err := sc.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("%s: no %s", path, key)
}

func readLoad1(path string) (float64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0, fmt.Errorf("%s: empty", path)
	}
	return strconv.ParseFloat(fields[0], 64)
}

// Score ranks machines for placement: free RAM minus a load penalty, or RAM
// only when ramOnly (--needs-ram). Higher is better.
func Score(m Metrics, loadWeightMB int, ramOnly bool) int {
	if ramOnly {
		return m.FreeMB
	}
	return m.FreeMB - int(float64(loadWeightMB)*m.Load1)
}

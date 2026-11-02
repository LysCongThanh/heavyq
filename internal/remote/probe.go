package remote

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/metrics"
)

// probeScript prints "heavyq-probe <free_mb> <load1> <cores> <busy,slots>" on
// Linux. On macOS (no /proc, no flock) it prints a "heavyq-probe-darwin <busy>"
// line followed by the raw `sysctl` and `vm_stat` output, which parseProbe
// feeds to metrics.ParseDarwin (the formulas of the local macOS sampler).
// Args: <work_dir> <slot names...>. A slot is busy when its lock file is held;
// Perl ships with macOS and its flock() is the same flock(2) the job holds.
const probeScript = `W="$1"; shift
case "$W" in /*) ;; *) W="$HOME/$W" ;; esac
if [ "$(uname -s 2>/dev/null)" = Darwin ]; then
PATH="$PATH:/usr/sbin:/sbin:/usr/bin:/bin"
b=""
for s in "$@"; do f="$W/slots/$s.lock"; if [ -e "$f" ] && ! perl -MFcntl=:flock -e 'open(F,"<",$ARGV[0]) or exit 1; exit(flock(F,LOCK_EX|LOCK_NB)?0:1)' "$f" 2>/dev/null; then b="$b,$s"; fi; done
b="${b#,}"
echo "heavyq-probe-darwin ${b:--}"
LC_ALL=C sysctl -n hw.memsize hw.ncpu vm.loadavg 2>/dev/null
LC_ALL=C vm_stat 2>/dev/null
exit 0
fi
m=$(awk '/^MemAvailable:/ {printf "%d", $2/1024}' /proc/meminfo 2>/dev/null)
l=$(cut -d' ' -f1 /proc/loadavg 2>/dev/null)
c=$(nproc 2>/dev/null || getconf _NPROCESSORS_ONLN 2>/dev/null)
b=""
for s in "$@"; do f="$W/slots/$s.lock"; if [ -e "$f" ] && ! flock -n "$f" true 2>/dev/null; then b="$b,$s"; fi; done
b="${b#,}"
echo "heavyq-probe ${m:--} ${l:--} ${c:-1} ${b:--}"
`

// SlotNames returns the remote slot names of kind light (true) or heavy.
func SlotNames(m config.MachineConfig, light bool) []string {
	n, prefix := m.HeavySlots, "h"
	if light {
		n, prefix = m.LightSlots, "l"
	}
	out := make([]string, n)
	for i := range out {
		out[i] = prefix + strconv.Itoa(i+1)
	}
	return out
}

// ProbeResult is one remote measurement.
type ProbeResult struct {
	Metrics metrics.Metrics
	Busy    map[string]bool
}

// Probe measures a remote machine. An error means unreachable.
func Probe(m config.MachineConfig, timeout time.Duration) (ProbeResult, error) {
	args := append([]string{m.WorkDir}, SlotNames(m, false)...)
	args = append(args, SlotNames(m, true)...)
	out, err := Transport(m.Transport).Output(timeout, probeScript, args...)
	if err != nil {
		return ProbeResult{}, err
	}
	return parseProbe(out)
}

func parseProbe(out string) (ProbeResult, error) {
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "heavyq-probe-darwin" {
			return parseProbeDarwin(f[1], lines[i+1:]), nil
		}
		if len(f) != 5 || f[0] != "heavyq-probe" {
			continue
		}
		r := ProbeResult{Busy: parseBusy(f[4])}
		free, errM := strconv.Atoi(f[1])
		load, errL := strconv.ParseFloat(f[2], 64)
		cores, errC := strconv.Atoi(f[3])
		if errC != nil || cores < 1 {
			cores = 1
		}
		r.Metrics = metrics.Metrics{FreeMB: free, Load1: load, Cores: cores, Known: errM == nil && errL == nil}
		return r, nil
	}
	return ProbeResult{}, fmt.Errorf("unexpected probe output %q", strings.TrimSpace(out))
}

// parseProbeDarwin reads the sysctl lines (memsize, ncpu, loadavg) and the
// vm_stat report that follow the marker line. Output that cannot be parsed
// leaves Known=false, so the gate degrades to slots only, as for a Linux
// remote whose /proc cannot be read.
func parseProbeDarwin(busy string, rest []string) ProbeResult {
	r := ProbeResult{Busy: parseBusy(busy), Metrics: metrics.Metrics{Cores: 1}}
	if len(rest) < 3 {
		return r
	}
	if m, err := metrics.ParseDarwin(strings.Join(rest[:3], "\n"), strings.Join(rest[3:], "\n")); err == nil {
		r.Metrics = m
	}
	return r
}

// parseBusy reads the comma-separated busy slot names ("-" = none).
func parseBusy(field string) map[string]bool {
	busy := map[string]bool{}
	if field != "-" {
		for _, s := range strings.Split(field, ",") {
			busy[s] = true
		}
	}
	return busy
}

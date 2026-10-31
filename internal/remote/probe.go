package remote

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/metrics"
)

// probeScript prints "heavyq-probe <free_mb> <load1> <cores> <busy,slots>".
// Args: <work_dir> <slot names...>. A slot is busy when its lock file is held.
const probeScript = `W="$1"; shift
case "$W" in /*) ;; *) W="$HOME/$W" ;; esac
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
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) != 5 || f[0] != "heavyq-probe" {
			continue
		}
		r := ProbeResult{Busy: map[string]bool{}}
		free, errM := strconv.Atoi(f[1])
		load, errL := strconv.ParseFloat(f[2], 64)
		cores, errC := strconv.Atoi(f[3])
		if errC != nil || cores < 1 {
			cores = 1
		}
		r.Metrics = metrics.Metrics{FreeMB: free, Load1: load, Cores: cores, Known: errM == nil && errL == nil}
		if f[4] != "-" {
			for _, s := range strings.Split(f[4], ",") {
				r.Busy[s] = true
			}
		}
		return r, nil
	}
	return ProbeResult{}, fmt.Errorf("unexpected probe output %q", strings.TrimSpace(out))
}

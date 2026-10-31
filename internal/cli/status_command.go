package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/procinfo"
	"github.com/LysCongThanh/heavyq/internal/queue"
	"github.com/LysCongThanh/heavyq/internal/remote"
	"github.com/LysCongThanh/heavyq/internal/slots"
)

// statusCommand prints the queue, machines, slot holders, CPU attribution and
// why the head ticket is waiting.
func statusCommand(cfg *config.Config, w io.Writer, stderr io.Writer) int {
	q, err := queue.Open(QueueDir())
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	probes := newProbeCache(cfg.Queue.ProbeTimeout.Duration)
	var all []string
	for _, r := range cfg.Remotes {
		all = append(all, r.Name)
	}
	probes.refresh(cfg, all) // before reading the queue: probes may be slow
	q.Prune(procinfo.Default.Alive)
	snap, err := q.List()
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	now := time.Now()

	fmt.Fprintln(w, "RUNNING")
	if len(snap.Running) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for _, t := range snap.Running {
		fmt.Fprintf(w, "  %-12s p%d  %-16s %8s  pid %-7d %s\n", t.Lane, t.Prio, t.Machine+"/"+t.Slot,
			fmtDur(now.Sub(t.ClaimedAt)), t.Pid, t.Cmd)
	}
	fmt.Fprintln(w, "WAITING (turn order)")
	if len(snap.Waiting) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for i, t := range snap.Waiting {
		kind := t.Target
		if t.Light {
			kind += ",light"
		}
		fmt.Fprintf(w, "  %d. %-10s p%d  %-13s %8s  pid %-7d %s\n", i+1, t.Lane, t.Prio, kind,
			fmtDur(now.Sub(t.Since)), t.Pid, t.Cmd)
	}

	fmt.Fprintln(w, "MACHINES")
	printLocal(w, cfg, snap)
	for _, r := range cfg.Remotes {
		printRemote(w, cfg, r, snap, probes)
	}
	if len(snap.Waiting) > 0 {
		printHeadReasons(w, cfg, snap, probes)
	}
	return 0
}

func printLocal(w io.Writer, cfg *config.Config, snap queue.Snapshot) {
	v := buildView(cfg, config.LocalName, snap, nil, cfg.Queue.HoldFor.Duration)
	fmt.Fprintf(w, "  %-10s %s\n", "local", v.state.Metrics)
	holders := slotHolders(snap, config.LocalName)
	var parts []string
	for _, light := range []bool{false, true} {
		for _, p := range localSlotPaths(cfg, light) {
			n := slotName(p)
			switch {
			case holders[n] != "":
				parts = append(parts, n+" "+holders[n])
			case slots.Busy(p):
				parts = append(parts, n+" busy (outside the queue)")
			default:
				parts = append(parts, n+" free")
			}
		}
	}
	fmt.Fprintf(w, "  %-10s slots: %s\n", "", strings.Join(parts, " · "))
	fmt.Fprintf(w, "  %-10s load by lane: %s\n", "", loadHolders(snap))
}

func printRemote(w io.Writer, cfg *config.Config, m config.MachineConfig, snap queue.Snapshot, probes *probeCache) {
	res, ok := probes.get(m.Name)
	if !ok {
		fmt.Fprintf(w, "  %-10s unreachable via %s\n", m.Name, strings.Join(m.Transport, " "))
		return
	}
	fmt.Fprintf(w, "  %-10s %s\n", m.Name, res.Metrics)
	holders := slotHolders(snap, m.Name)
	var parts []string
	for _, light := range []bool{false, true} {
		for _, s := range remote.SlotNames(m, light) {
			switch {
			case holders[s] != "":
				parts = append(parts, s+" "+holders[s])
			case res.Busy[s]:
				parts = append(parts, s+" busy (outside the queue)")
			default:
				parts = append(parts, s+" free")
			}
		}
	}
	fmt.Fprintf(w, "  %-10s slots: %s\n", "", strings.Join(parts, " · "))
}

// slotHolders maps slot name -> lane of the running ticket holding it.
func slotHolders(snap queue.Snapshot, machine string) map[string]string {
	out := map[string]string{}
	for _, t := range snap.Running {
		if t.Machine == machine && t.Slot != "" {
			out[t.Slot] = t.Lane
		}
	}
	return out
}

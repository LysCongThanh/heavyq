package cli

import (
	"bytes"
	"encoding/json"
	"flag"
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
// why the head ticket is waiting; with --json, the schema-1 snapshot.
func statusCommand(cfg *config.Config, args []string, w io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the machine-readable snapshot (schema 1)")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *asJSON {
		s, err := buildSnapshot(cfg, newProbeCache(cfg.Queue.ProbeTimeout.Duration), cpuSampleInterval)
		if err != nil {
			fmt.Fprintln(stderr, "heavyq:", err)
			return ExitUsage
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(s); err != nil {
			fmt.Fprintln(stderr, "heavyq:", err)
			return 1
		}
		return 0
	}
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
	// The story needs the same CPU sample as the lane breakdown below, so
	// both run at once: `status` stays one sample interval slow, not two.
	story := startStory(cfg, probes)
	var text bytes.Buffer
	if rc := printStatusText(&text, cfg, q, probes, stderr); rc != 0 {
		return rc
	}
	printStory(w, stderr, <-story)
	_, _ = w.Write(text.Bytes())
	return 0
}

// printStatusText writes the RUNNING, WAITING and MACHINES sections.
func printStatusText(w io.Writer, cfg *config.Config, q *queue.Dir, probes *probeCache, stderr io.Writer) int {
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
		printWith(w, t.With)
	}
	fmt.Fprintln(w, "WAITING (turn order)")
	if len(snap.Waiting) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for i, t := range snap.Waiting {
		kind := t.Target
		if t.Light {
			kind += " light" // not ",light": a target may itself be a list "a,b"
		}
		fmt.Fprintf(w, "  %d. %-10s p%d  %-13s %8s  pid %-7d %s\n", i+1, t.Lane, t.Prio, kind,
			fmtDur(now.Sub(t.Since)), t.Pid, t.Cmd)
		printWith(w, t.With)
	}
	printCoalesced(w, now)

	fmt.Fprintln(w, "MACHINES")
	printLocal(w, cfg, snap)
	for _, r := range cfg.Remotes {
		printRemote(w, cfg, r, snap, probes)
	}
	printPools(w, displayPoolSet(cfg), snap)
	if len(snap.Waiting) > 0 {
		printHeadReasons(w, cfg, snap, probes)
	}
	return 0
}

func printLocal(w io.Writer, cfg *config.Config, snap queue.Snapshot) {
	v := buildView(cfg, config.LocalName, snap, nil, cfg.Queue.HoldFor.Duration)
	fmt.Fprintf(w, "  %-10s %s%s\n", "local", v.state.Metrics, pausedNote(config.LocalName))
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
	fmt.Fprintf(w, "  %-10s %s%s\n", m.Name, res.Metrics, pausedNote(m.Name))
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

// pausedNote marks a machine whose admissions an operator paused.
func pausedNote(machine string) string {
	if controlBox().Paused(machine) {
		return "  [PAUSED: no new jobs start here]"
	}
	return ""
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

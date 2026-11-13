package fleet

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/metrics"
)

// Probe is what one machine answered.
type Probe struct {
	OS       string
	Cores    int
	Load1    float64
	TotalMB  int
	FreeMB   int
	Pressure *Pressure // nil when the kernel has no /proc/pressure (or on macOS)
	WorkDir  string    // heavyq's work dir there, resolved ("" when none was asked)
	Procs    []Proc
	// Args are the command lines of the busiest processes, by pid (not yet redacted).
	Args map[int]string
	// InWorkTree marks the processes whose cwd is in <work_dir>/wt: heavyq remote jobs.
	InWorkTree map[int]bool
	Slots      []SlotState
	// Partial says what the probe could not measure in time and why ("" when it measured everything).
	Partial string
	self    int // the probe script's own pid: it and its children (ps, awk) are not the machine's load
	// The Linux CPU sample: clock ticks per second, the sample's length, ticks used per pid.
	clk        float64
	intervalNS float64
	ticks      map[int]float64
}

// Proc is one process of a probe.
type Proc struct {
	Pid, PPid int
	Threads   int     // 0 when unknown (macOS)
	CPU       float64 // ps %CPU: one core = 100
	RSSKB     int
	Comm      string
}

// SlotState is one slot lock file of a queue on a machine.
type SlotState struct {
	Owner  string
	Name   string
	Busy   bool
	Holder int // pid holding the lock (0: free or unknown)
}

// ParseProbe reads the whole output of fleetProbeScript. Lines before the marker (login banners) are
// ignored; output without it, or without the machine's numbers, is an error.
func ParseProbe(out string) (Probe, error) { return parseProbe(out, "", false) }

// parseProbe reads what the script printed; cut is why it was stopped before its end ("" when it was not),
// and then the last line, unless complete, is dropped.
func parseProbe(out, cut string, wantWT bool) (Probe, error) {
	p := Probe{Args: map[int]string{}, InWorkTree: map[int]bool{}, ticks: map[int]float64{}}
	steps := stepState{done: map[string]bool{}, skipped: map[string]string{}}
	if cut != "" {
		out = out[:strings.LastIndexByte(out, '\n')+1]
	}
	lines := strings.Split(out, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "heavyq-fleet 1" {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return Probe{}, fmt.Errorf("unexpected probe output %q", clip(strings.TrimSpace(out), 120))
	}
	var darwin []string
	inDarwin := false
	for _, l := range lines[start:] {
		switch {
		case l == "darwin-begin":
			inDarwin = true
		case l == "darwin-end":
			inDarwin = false
		case inDarwin:
			darwin = append(darwin, l)
		case !steps.stepLine(strings.Fields(l)):
			p.line(l)
		}
	}
	if err := p.settle(steps, cut, wantWT); err != nil {
		return Probe{}, err
	}
	p.Procs = withoutTree(p.Procs, p.self)
	p.applySample()
	if len(darwin) >= 3 {
		if m, err := metrics.ParseDarwin(strings.Join(darwin[:3], "\n"), strings.Join(darwin[3:], "\n")); err == nil {
			p.Cores, p.Load1, p.FreeMB, p.TotalMB = m.Cores, m.Load1, m.FreeMB, m.TotalMB
		}
	}
	return p, nil
}

// line reads one fact line into p; unknown lines are ignored (a newer script may say more).
func (p *Probe) line(l string) {
	if rest, ok := strings.CutPrefix(l, "a\t"); ok {
		pid, args, _ := strings.Cut(rest, "\t")
		if n, err := strconv.Atoi(pid); err == nil {
			p.Args[n] = strings.TrimSpace(args)
		}
		return
	}
	f := strings.Fields(l)
	if len(f) < 2 {
		return
	}
	switch f[0] {
	case "os":
		p.OS = f[1]
	case "self":
		p.self, _ = strconv.Atoi(f[1])
	case "clk":
		p.clk, _ = strconv.ParseFloat(f[1], 64)
	case "interval_ns":
		p.intervalNS, _ = strconv.ParseFloat(f[1], 64)
	case "c":
		if len(f) == 3 {
			pid, err1 := strconv.Atoi(f[1])
			t, err2 := strconv.ParseFloat(f[2], 64)
			if err1 == nil && err2 == nil {
				p.ticks[pid] = t
			}
		}
	case "workdir":
		p.WorkDir = strings.TrimPrefix(l, "workdir ")
	case "cores":
		p.Cores, _ = strconv.Atoi(f[1])
	case "load":
		p.Load1, _ = strconv.ParseFloat(f[1], 64)
	case "mem_total_mb":
		p.TotalMB, _ = strconv.Atoi(f[1])
	case "mem_avail_mb":
		p.FreeMB, _ = strconv.Atoi(f[1])
	case "psi_cpu", "psi_memory":
		p.pressure(f)
	case "p":
		if pr, ok := parseProc(f[1:]); ok {
			p.Procs = append(p.Procs, pr)
		}
	case "w":
		if n, err := strconv.Atoi(f[1]); err == nil {
			p.InWorkTree[n] = true
		}
	case "s":
		if len(f) == 5 {
			h, _ := strconv.Atoi(f[4])
			p.Slots = append(p.Slots, SlotState{Owner: f[1], Name: f[2], Busy: f[3] == "1", Holder: h})
		}
	}
}

// pressure reads "psi_<res> <some|full> avg10=x avg60=y ...".
func (p *Probe) pressure(f []string) {
	if len(f) < 3 || !strings.HasPrefix(f[2], "avg10=") {
		return
	}
	v, err := strconv.ParseFloat(strings.TrimPrefix(f[2], "avg10="), 64)
	if err != nil {
		return
	}
	if p.Pressure == nil {
		p.Pressure = &Pressure{}
	}
	switch f[0] + " " + f[1] {
	case "psi_cpu some":
		p.Pressure.CPUSomeAvg10 = v
	case "psi_memory some":
		p.Pressure.MemSomeAvg10 = v
	case "psi_memory full":
		p.Pressure.MemFullAvg10 = v
	}
}

// parseProc reads "<pid> <ppid> <threads> <%cpu> <rss_kb> <comm...>".
func parseProc(f []string) (Proc, bool) {
	if len(f) < 6 {
		return Proc{}, false
	}
	pid, e1 := strconv.Atoi(f[0])
	ppid, e2 := strconv.Atoi(f[1])
	thr, e3 := strconv.Atoi(f[2])
	cpu, e4 := strconv.ParseFloat(f[3], 64)
	rss, e5 := strconv.Atoi(f[4])
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil {
		return Proc{}, false
	}
	return Proc{Pid: pid, PPid: ppid, Threads: thr, CPU: cpu, RSSKB: rss, Comm: strings.Join(f[5:], " ")}, true
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

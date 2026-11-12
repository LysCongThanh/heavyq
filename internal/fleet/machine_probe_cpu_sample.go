package fleet

// applySample replaces ps's %CPU (CPU time over the whole life of a process, so a fresh `ps` reads 900%
// and an idle daemon that once worked reads high) with the sampled one, when the probe took a sample
// (Linux): ticks used during the sample / clock ticks per second / the sample's seconds.
func (p *Probe) applySample() {
	if p.clk <= 0 {
		return
	}
	secs := p.intervalNS / 1e9
	if secs < 0.2 || secs > 30 {
		secs = 1 // the script sleeps 1 s; a clock without nanoseconds gives no interval
	}
	for i := range p.Procs {
		p.Procs[i].CPU = round2(p.ticks[p.Procs[i].Pid] / p.clk / secs * 100)
	}
}

// withoutTree drops the process root and its descendants (the probe script and the ps, awk and sleep it
// started: they are the measurement, not the machine's load).
func withoutTree(procs []Proc, root int) []Proc {
	if root <= 0 {
		return procs
	}
	drop := map[int]bool{root: true}
	for changed := true; changed; {
		changed = false
		for _, pr := range procs {
			if !drop[pr.Pid] && drop[pr.PPid] {
				drop[pr.Pid], changed = true, true
			}
		}
	}
	out := procs[:0]
	for _, pr := range procs {
		if !drop[pr.Pid] {
			out = append(out, pr)
		}
	}
	return out
}

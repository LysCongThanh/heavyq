package render

import (
	"fmt"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/snapshot"
	"github.com/LysCongThanh/heavyq/internal/tui/width"
)

const (
	barCells    = 8
	maxNameW    = 10
	outsideMinM = 256 // an outsider smaller than this is not worth a name
)

// machineSection is MACHINES: a row per machine with a RAM bar (the marker
// is the free RAM one heavy job needs), load against its limit, and what is
// worth saying. detailed adds a line of slots under each machine.
func (c *ctx) machineSection(rows []Row, w int, detailed bool) []bodyLine {
	out := []bodyLine{{Line: c.heading("MACHINES", 0, w)}}
	nameW := 5
	for _, r := range rows {
		if r.Kind == RowMachine {
			nameW = min(maxNameW, max(nameW, width.String(c.th.txt(r.Machine))))
		}
	}
	for _, r := range rows {
		if r.Kind != RowMachine {
			continue
		}
		out = append(out, c.row(r.Key, c.machineContent(*r.Mach, nameW, w-2), w))
		if detailed {
			out = append(out, c.slotLine(*r.Mach, nameW))
		}
	}
	return out
}

func (c *ctx) machineContent(m snapshot.Machine, nameW, w int) Line {
	l := Line{Seg{width.Pad(width.Truncate(c.th.txt(m.Name), nameW, c.th.g(gEll)), nameW+2), Bold}}
	if !m.Reachable {
		return append(l, c.seg(Bad|Bold, "unreachable"))
	}
	need := m.JobMB + m.ReserveMB + m.ReservedMB
	scale := m.TotalMB
	if scale <= 0 {
		scale = max(m.FreeMB, 2*need)
	}
	l = append(l, bar(c.th, m.FreeMB, scale, need, barCells)...)
	l = append(l, blanks(2), c.seg(0, size(m.FreeMB)+" free"))
	if m.MetricsKnown {
		st := Style(0)
		if m.MaxLoad > 0 && m.Load1 >= m.MaxLoad {
			st = Warn
		}
		l = append(l, blanks(2), c.seg(st, fmt.Sprintf("load %.1f/%.1f", m.Load1, m.MaxLoad)))
	} else {
		l = append(l, blanks(2), c.seg(Dim, "metrics unknown"))
	}
	// Chips in order of importance; each is added only if it fits whole.
	for _, ch := range c.chips(m) {
		if l.Width()+2+ch.Width() <= w {
			l = append(append(l, blanks(2)), ch...)
		}
	}
	return l
}

// chips are the optional facts of a machine, most important first.
func (c *ctx) chips(m snapshot.Machine) []Line {
	var out []Line
	if m.Paused {
		out = append(out, Line{c.seg(Warn|Bold, "PAUSED")})
	}
	if m.Admits != "" {
		out = append(out, Line{c.seg(Warn, "! "+shortReason(m.Admits))})
	}
	if s := slotSummary(m); s != "" {
		out = append(out, Line{c.seg(Dim, s)})
	}
	if m.Name == "local" && c.s.CPU.Available && c.s.CPU.OutsideTotal.RSSMB > 0 {
		out = append(out, Line{c.seg(0, "outside "+size(c.s.CPU.OutsideTotal.RSSMB))})
		if o := c.s.CPU.Outside; len(o) > 0 && o[0].RSSMB >= outsideMinM {
			out = append(out, Line{c.seg(Dim, o[0].Name+" "+size(o[0].RSSMB))})
		}
	}
	return out
}

// slotSummary is "slots free h 1/2 l 1/1": free of total, heavy then light.
func slotSummary(m snapshot.Machine) string {
	var hf, ht, lf, lt int
	for _, s := range m.Slots {
		free := 0
		if s.State == snapshot.SlotFree {
			free = 1
		}
		if s.Kind == "light" {
			lf, lt = lf+free, lt+1
		} else {
			hf, ht = hf+free, ht+1
		}
	}
	var parts []string
	if ht > 0 {
		parts = append(parts, fmt.Sprintf("h %d/%d", hf, ht))
	}
	if lt > 0 {
		parts = append(parts, fmt.Sprintf("l %d/%d", lf, lt))
	}
	if len(parts) == 0 {
		return ""
	}
	return "slots free " + strings.Join(parts, " ")
}

// slotLine lists each slot of a machine and who holds it (machines mode).
func (c *ctx) slotLine(m snapshot.Machine, nameW int) bodyLine {
	var l Line
	l = append(l, blanks(nameW+2))
	for i, s := range m.Slots {
		if i > 0 {
			l = append(l, blanks(2))
		}
		switch s.State {
		case snapshot.SlotHeld:
			l = append(l, c.seg(0, shortSlot(s.Name)+" "+s.Lane))
		case snapshot.SlotOutside:
			l = append(l, c.seg(Warn, shortSlot(s.Name)+" outside"))
		default:
			l = append(l, c.seg(Dim, shortSlot(s.Name)+" free"))
		}
	}
	return c.plainLine(l...)
}

// poolSection lists the named pools and who holds their values; empty when
// there are none. detailed names the holders.
func (c *ctx) poolSection(w int, detailed bool) []bodyLine {
	if len(c.s.Pools) == 0 {
		return nil
	}
	out := []bodyLine{{Line: c.heading("POOLS", 0, w)}}
	var inline Line
	for _, p := range c.s.Pools {
		st := Style(0)
		if p.Capacity > 0 && p.Held >= p.Capacity {
			st = Warn
		}
		text := fmt.Sprintf("%s %d/%d", p.Name, p.Held, p.Capacity)
		if detailed {
			var who []string
			for _, h := range p.Holders {
				who = append(who, h.Value+" "+h.Lane)
			}
			if len(who) > 0 {
				text += "  " + strings.Join(who, ", ")
			}
			out = append(out, c.plainLine(c.seg(st, text)))
			continue
		}
		if inline.Width()+cellWidth(text)+2 > w-2 {
			out = append(out, c.plainLine(inline...))
			inline = nil
		}
		if len(inline) > 0 {
			inline = append(inline, blanks(2))
		}
		inline = append(inline, c.seg(st, text))
	}
	if len(inline) > 0 {
		out = append(out, c.plainLine(inline...))
	}
	return out
}

// outsideSection names what runs outside the queue and takes the machine's
// RAM (machines mode); empty when nothing is measured.
func (c *ctx) outsideSection(w int) []bodyLine {
	cpu := c.s.CPU
	if !cpu.Available || len(cpu.Outside) == 0 {
		return nil
	}
	out := []bodyLine{{Line: c.heading("OUTSIDE THE QUEUE", 0, w)}}
	for _, u := range cpu.Outside {
		out = append(out, c.plainLine(c.seg(0, c.usageText(u.Name, u.Procs, u.CPUPct, u.RSSMB))))
	}
	t := cpu.OutsideTotal
	out = append(out, c.plainLine(c.seg(Dim, c.usageText("total", t.Procs, t.CPUPct, t.RSSMB))))
	return out
}

// usageText is one aligned line of the outside-the-queue table.
func (c *ctx) usageText(name string, procs int, cpu float64, rssMB int) string {
	n := width.Pad(width.Truncate(c.th.txt(name), 14, c.th.g(gEll)), 14)
	return fmt.Sprintf("%s %3d procs  %5.0f%% cpu  %6s", n, procs, cpu, size(rssMB))
}

package render

import (
	"strconv"

	"github.com/LysCongThanh/heavyq/internal/tui/width"
)

// colID names a column of the lane table.
type colID int

const (
	colState colID = iota
	colLane
	colPrio
	colWhere
	colActive // time in the state it is in: running time, or waiting time
	colWait
	colRun
	colGate
	colItem
)

// Column sets. In the compact layout only state, lane and time stay.
var (
	colsFull    = []colID{colState, colLane, colPrio, colWhere, colActive}
	colsTable   = []colID{colState, colLane, colPrio, colWhere, colWait, colRun, colGate, colItem}
	colsCompact = []colID{colState, colLane, colActive}
	// dropOrder is which column goes first when the row is too narrow. The
	// table keeps the gate (its tree is what the lanes mode is for) longest.
	dropOrder      = []colID{colItem, colGate, colWait, colWhere, colPrio}
	tableDropOrder = []colID{colItem, colPrio, colWhere, colWait, colGate}
)

const (
	maxLaneW  = 16
	maxWhereW = 14
	maxItemW  = 16
	timeW     = 6
	minDetail = 16 // narrowest useful command column
)

var titles = map[colID]string{colState: "state", colLane: "lane", colPrio: "p", colWhere: "where",
	colActive: "time", colWait: "wait", colRun: "run", colGate: "gate", colItem: "item"}

// laneSection is the LANES heading and a row per ticket. withTitles adds a
// column header row (the lanes mode). cols is the wanted column set; columns
// that do not fit are dropped, least important first.
func (c *ctx) laneSection(rows []Row, w int, cols []colID, withTitles bool) []bodyLine {
	var tickets []Row
	for _, r := range rows {
		if r.Kind == RowTicket {
			tickets = append(tickets, r)
		}
	}
	title := "LANES"
	if c.v.Filter != "" {
		title += " matching " + strconv.Quote(c.v.Filter)
	}
	out := []bodyLine{{Line: c.heading(title, 0, w)}}
	if len(tickets) == 0 {
		msg := "no jobs running or waiting"
		if c.v.Filter != "" {
			msg = "no lane matches the filter"
		}
		return append(out, c.plainLine(c.seg(Dim, msg)))
	}
	fs := make([]facts, len(tickets))
	for i, r := range tickets {
		fs[i] = c.factsOf(r)
	}
	widths := columnWidths(c, fs)
	cols = fitColumns(cols, widths, w-2, withTitles)
	if withTitles {
		out = append(out, c.plainLine(c.titleCells(cols, widths)...))
	}
	flexW := w - 2 - tableWidth(cols, widths)
	for i, r := range tickets {
		l := c.cells(fs[i], cols, widths)
		if len(cols) > 0 && flexW >= minDetail {
			d := c.detail(fs[i], flexW)
			if !withTitles { // the table has its own gate and item columns
				d = c.withRight(d, fs[i], flexW)
			}
			l = append(l, d...)
		}
		out = append(out, c.row(r.Key, l, w))
	}
	return out
}

// withRight appends gate and item to the detail when plenty of room is left.
func (c *ctx) withRight(d Line, f facts, flexW int) Line {
	var right Line
	if f.gate != "" {
		right = append(right, c.seg(0, "gate "), c.seg(f.gateSt, f.gate))
	}
	if f.item != "" {
		if len(right) > 0 {
			right = append(right, blanks(2))
		}
		right = append(right, c.seg(0, "item "), c.seg(f.itemSt, f.item))
	}
	if len(right) == 0 || flexW-d.Width()-2 < right.Width()+10 {
		return d
	}
	gap := flexW - d.Width() - right.Width()
	return append(append(d, blanks(gap)), right...)
}

// columnWidths measures the data-driven columns.
func columnWidths(c *ctx, fs []facts) map[colID]int {
	w := map[colID]int{colState: 6, colPrio: 2, colActive: timeW, colWait: timeW, colRun: timeW,
		colLane: 4, colWhere: 5, colGate: 4, colItem: 4}
	for _, f := range fs {
		w[colLane] = min(maxLaneW, max(w[colLane], width.String(c.th.txt(f.lane))))
		w[colWhere] = min(maxWhereW, max(w[colWhere], width.String(c.th.txt(f.where))))
		w[colGate] = max(w[colGate], width.String(f.gate))
		w[colItem] = min(maxItemW, max(w[colItem], width.String(c.th.txt(f.item))))
	}
	return w
}

// fitColumns drops columns in dropOrder until the table leaves room for a command.
func fitColumns(cols []colID, widths map[colID]int, avail int, table bool) []colID {
	cols = append([]colID(nil), cols...)
	order := dropOrder
	if table {
		order = tableDropOrder
	}
	for _, d := range order {
		if tableWidth(cols, widths)+minDetail <= avail {
			break
		}
		for i, id := range cols {
			if id == d {
				cols = append(cols[:i], cols[i+1:]...)
				break
			}
		}
	}
	return cols
}

// tableWidth is the cells the columns take, with a two-space gap after each.
func tableWidth(cols []colID, widths map[colID]int) int {
	n := 0
	for _, id := range cols {
		n += widths[id] + 2
	}
	return n
}

func (c *ctx) titleCells(cols []colID, widths map[colID]int) []Seg {
	var out []Seg
	for _, id := range cols {
		out = append(out, Seg{width.Pad(titles[id], widths[id]+2), Dim})
	}
	return out
}

// cells renders one ticket's columns.
func (c *ctx) cells(f facts, cols []colID, widths map[colID]int) Line {
	var l Line
	cell := func(id colID, text string, st Style) {
		t := width.Truncate(c.th.txt(text), widths[id], c.th.g(gEll))
		l = append(l, Seg{width.Pad(t, widths[id]+2), st})
	}
	for _, id := range cols {
		switch id {
		case colState:
			if f.running {
				l = append(l, c.mark(gRun, Good), Seg{width.Pad("run", 4), Good})
			} else {
				l = append(l, c.mark(gWait, Warn), Seg{width.Pad("wait", 4), Warn})
			}
			l = append(l, blanks(2))
		case colLane:
			cell(id, f.lane, Bold)
		case colPrio:
			cell(id, "p"+strconv.Itoa(f.prio), Dim)
		case colWhere:
			cell(id, f.where, 0)
		case colActive:
			if f.running {
				cell(id, dur(f.runS), 0)
			} else {
				cell(id, dur(f.waitS), Warn)
			}
		case colWait:
			cell(id, dur(f.waitS), 0)
		case colRun:
			if f.running {
				cell(id, dur(f.runS), 0)
			} else {
				cell(id, "-", Dim)
			}
		case colGate:
			cell(id, f.gate, f.gateSt)
		case colItem:
			cell(id, f.item, f.itemSt)
		}
	}
	return l
}

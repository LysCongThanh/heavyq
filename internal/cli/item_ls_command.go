package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"time"

	"github.com/LysCongThanh/heavyq/internal/anchor"
	"github.com/LysCongThanh/heavyq/internal/itemstatus"
	"github.com/LysCongThanh/heavyq/internal/redact"
)

// maxTerseItemRows keeps `item ls` inside an agent's output budget.
const maxTerseItemRows = 25

// itemJSON is one item of `item ls --json`: the anchor line and its status,
// flattened. Item text is data written by whoever edited the anchor.
type itemJSON struct {
	itemstatus.Status
	Text     string       `json:"text"`
	Checked  bool         `json:"checked"`
	Proof    anchor.Proof `json:"proof"`
	Source   string       `json:"source"`
	Line     int          `json:"line"`
	ReadOnly bool         `json:"read_only"`
}

// itemListJSON is the `item ls --json` document.
type itemListJSON struct {
	Schema   int        `json:"schema"`
	Goal     string     `json:"goal"`
	Anchor   string     `json:"anchor"`
	Lane     string     `json:"lane"` // who is asking
	Items    []itemJSON `json:"items"`
	Warnings []string   `json:"warnings"`
}

const itemListSchema = 1

// itemLs implements `heavyq item ls [--all] [--json]`. Without --all the
// finished items (proven and ticked, or ticked and still proven) are left out.
func itemLs(c *itemCtx, args []string) int {
	fs := flag.NewFlagSet("item ls", flag.ContinueOnError)
	all := fs.Bool("all", false, "include items that are proven and ticked")
	asJSON := fs.Bool("json", false, "machine-readable output (schema in docs/items.md)")
	if pos, ok := parseItemArgs(fs, args, c.stderr); !ok || len(pos) > 0 {
		fmt.Fprint(c.stderr, itemUsage)
		return ExitUsage
	}
	rows, err := c.rows(ledgerOrNone(c.stderr), time.Now(), false)
	if err != nil {
		fmt.Fprintln(c.stderr, "heavyq:", err)
		return 1
	}
	if !*all {
		rows = unfinished(rows)
	}
	if *asJSON {
		return c.listJSON(rows)
	}
	c.listText(rows)
	return 0
}

// unfinished drops the items that are done and still proven.
func unfinished(rows []itemRow) []itemRow {
	out := rows[:0:0]
	for _, r := range rows {
		if r.Status.State == itemstatus.Proven && r.Status.Done {
			continue
		}
		out = append(out, r)
	}
	return out
}

func (c *itemCtx) listJSON(rows []itemRow) int {
	doc := itemListJSON{Schema: itemListSchema, Goal: c.file.Goal, Anchor: c.file.Path, Lane: c.lane,
		Items: make([]itemJSON, 0, len(rows)), Warnings: []string{}}
	for _, w := range c.file.Warnings {
		doc.Warnings = append(doc.Warnings, w.String())
	}
	for _, r := range rows {
		p := r.Item.Proof
		p.Value = redact.Command(p.Value)
		doc.Items = append(doc.Items, itemJSON{Status: r.Status, Text: r.Item.Text, Checked: r.Item.Checked,
			Proof: p, Source: r.Item.Source, Line: r.Item.Line, ReadOnly: r.Item.ReadOnly})
	}
	enc := json.NewEncoder(c.stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		fmt.Fprintln(c.stderr, "heavyq:", err)
		return 1
	}
	return 0
}

func (c *itemCtx) listText(rows []itemRow) {
	if c.file.Goal != "" {
		fmt.Fprintf(c.stdout, "goal: %s\n", oneLine(c.file.Goal, 120))
	}
	if len(rows) == 0 {
		fmt.Fprintf(c.stdout, "no open items in %s\n", c.file.Path)
	}
	shown := rows
	if c.terse && len(shown) > maxTerseItemRows {
		shown = shown[:maxTerseItemRows]
	}
	for _, r := range shown {
		s := r.Status
		fmt.Fprintf(c.stdout, "%-22s %-21s %s\n", oneLine(s.ID, 22), s.State+statusDetail(s), oneLine(r.Item.Text, 70))
	}
	if len(shown) < len(rows) {
		fmt.Fprintf(c.stdout, "... %d more (heavyq item ls --json)\n", len(rows)-len(shown))
	}
	if c.terse {
		if next := nextItemStep(c.lane, rows); next != "" {
			fmt.Fprintln(c.stdout, "next: "+next)
		}
	}
}

// statusDetail is the parenthesised part after the state: lane and expiry,
// commit and time, or the need.
func statusDetail(s itemstatus.Status) string {
	switch s.State {
	case itemstatus.Claimed:
		return fmt.Sprintf(" (%s, until %s)", s.Lane, s.Expires.Local().Format("15:04"))
	case itemstatus.Proven:
		d := fmt.Sprintf(" (%s, %s)", s.Commit, s.At.Local().Format("15:04"))
		if s.Lane != "" {
			d = fmt.Sprintf(" (%s, %s, %s)", s.Lane, s.Commit, s.At.Local().Format("15:04"))
		}
		return d
	case itemstatus.Stale:
		return fmt.Sprintf(" (%s, older tree)", s.Commit)
	case itemstatus.Blocked:
		return " (needs: " + oneLine(s.Needs, 40) + ")"
	}
	return ""
}

// nextItemStep is the one thing the asking lane should do: finish what it
// holds, else take the next item. "" when there is nothing to do.
func nextItemStep(lane string, rows []itemRow) string {
	for _, r := range rows {
		if r.Status.Lane != lane {
			continue
		}
		switch r.Status.State {
		case itemstatus.Proven:
			return "heavyq item done " + r.Status.ID
		case itemstatus.Claimed:
			return "heavyq item done " + r.Status.ID + " --run"
		}
	}
	for _, r := range rows {
		if _, next := itemstatus.Claimable(r.Status, r.Item.Needs != ""); next {
			return "heavyq item claim --next"
		}
	}
	return ""
}

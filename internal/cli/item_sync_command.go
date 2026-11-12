package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/itemstatus"
	"github.com/LysCongThanh/heavyq/internal/lease"
)

// syncJSON is the `item sync --json` document.
type syncJSON struct {
	Synced []string `json:"synced"` // the items whose box was ticked, in anchor order
	Source string   `json:"source"`
}

// itemSync implements `heavyq item sync [--json]`: it ticks, in the anchor file
// of this work tree, the box of every item that `item done` finished without
// ticking it (`[items] tick`, or parallel work trees, #119). The events log is
// shared by every work tree, so the lead runs it once where the anchor is
// committed (main, after the lanes landed), instead of every lane editing the
// same file. It never writes a plan-file item, and never touches an item that
// was claimed again since its done. Under the lease store's lock, one byte per
// box; running it twice changes nothing.
func itemSync(c *itemCtx, args []string) int {
	fs := flag.NewFlagSet("item sync", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "machine-readable output")
	if pos, ok := parseItemArgs(fs, args, c.stderr); !ok || len(pos) > 0 {
		fmt.Fprint(c.stderr, itemUsage)
		return ExitUsage
	}
	synced := []string{}
	err := c.store.WithLock(func() error {
		events, err := c.store.Events(eventsWindow)
		if err != nil {
			return err
		}
		marks := itemstatus.Marks(events)
		owner := lease.Owner{Lane: c.lane, Session: c.session}
		for _, it := range c.file.Items {
			m := marks[it.ID]
			if it.ReadOnly || it.Checked || !m.Done || !m.Deferred {
				continue
			}
			if err := tickItem(c.root, it); err != nil {
				return fmt.Errorf("item %s: %w", it.ID, err)
			}
			synced = append(synced, it.ID)
			if err := c.store.NoteSync(it.ID, owner, m.Commit); err != nil {
				return fmt.Errorf("item %s: %w", it.ID, err)
			}
		}
		return nil
	})
	if err != nil {
		if len(synced) > 0 {
			fmt.Fprintf(c.stderr, "heavyq: ticked %s before the error; run item sync again\n", strings.Join(synced, ", "))
		}
		fmt.Fprintln(c.stderr, "heavyq: item sync:", err)
		return 1
	}
	if *asJSON {
		enc := json.NewEncoder(c.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(syncJSON{Synced: synced, Source: c.file.Path}); err != nil {
			fmt.Fprintln(c.stderr, "heavyq:", err)
			return 1
		}
		return 0
	}
	if len(synced) == 0 {
		fmt.Fprintf(c.stdout, "nothing to sync: every item finished with heavyq item done is ticked in %s\n", c.file.Path)
		if c.terse {
			fmt.Fprintln(c.stdout, "next: heavyq item claim --next")
		}
		return 0
	}
	fmt.Fprintf(c.stdout, "synced %s: ticked [x] in %s (commit it)\n", strings.Join(synced, ", "), c.file.Path)
	if c.terse {
		fmt.Fprintln(c.stdout, "next: commit "+c.file.Path)
	}
	return 0
}

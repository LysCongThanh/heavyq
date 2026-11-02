package cli

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/LysCongThanh/heavyq/internal/itemstatus"
	"github.com/LysCongThanh/heavyq/internal/lease"
)

// maxNeedsLen bounds the need recorded by `item block`.
const maxNeedsLen = 300

// itemRelease implements `heavyq item release <id> [--force]`: give the item
// back. Only the holder may, unless --force (a human freeing a stuck agent).
func itemRelease(c *itemCtx, args []string) int {
	fs := flag.NewFlagSet("item release", flag.ContinueOnError)
	force := fs.Bool("force", false, "release it even if another lane holds it")
	pos, ok := parseItemArgs(fs, args, c.stderr)
	if !ok || len(pos) != 1 {
		fmt.Fprint(c.stderr, itemUsage)
		return ExitUsage
	}
	id := pos[0]
	if !c.validID(id) { // not looked up in the anchor: a leftover lease of a removed item can be released too
		return ExitUsage
	}
	err := c.store.Release(id, c.ownerFor(id), *force)
	var held *lease.HeldError
	switch {
	case err == nil:
		fmt.Fprintf(c.stdout, "released %s\n", id)
		if c.terse {
			fmt.Fprintln(c.stdout, "next: heavyq item claim --next")
		}
		return 0
	case errors.As(err, &held):
		return c.refuse(fmt.Sprintf("%s (--force releases it anyway)", held.Error()), "heavyq item release "+id+" --force")
	case errors.Is(err, lease.ErrNotFound):
		return c.refuse(fmt.Sprintf("item %s is not claimed", id), "heavyq item ls")
	case errors.Is(err, lease.ErrEventLog):
		fmt.Fprintf(c.stdout, "released %s\n", id)
		fmt.Fprintln(c.stderr, "heavyq: warning:", err)
		return 0
	}
	fmt.Fprintln(c.stderr, "heavyq:", err)
	return 1
}

// itemBlock implements `heavyq item block <id> "needs: ..."`: say what a human
// must provide, and free the item. The block lasts until someone claims the
// item again.
func itemBlock(c *itemCtx, args []string) int {
	fs := flag.NewFlagSet("item block", flag.ContinueOnError)
	pos, ok := parseItemArgs(fs, args, c.stderr)
	if !ok || len(pos) != 2 {
		fmt.Fprint(c.stderr, itemUsage)
		return ExitUsage
	}
	id := pos[0]
	needs, err := cleanNeeds(pos[1])
	if err != nil {
		fmt.Fprintln(c.stderr, "heavyq:", err)
		return ExitUsage
	}
	rows, err := c.rows(ledgerOrNone(c.stderr), time.Now(), true)
	if err != nil {
		fmt.Fprintln(c.stderr, "heavyq:", err)
		return 1
	}
	row, ok := findRow(rows, id)
	if !ok {
		return c.unknownItem(id)
	}
	if row.Status.State == itemstatus.Proven {
		return c.refuse(fmt.Sprintf("item %s is already proven (%s): nothing blocks it", id, row.Status.Commit), "heavyq item done "+id)
	}
	err = c.store.Finish(id, c.ownerFor(id), lease.Event{Op: lease.OpBlock, Needs: needs}, nil)
	var held *lease.HeldError
	switch {
	case errors.As(err, &held):
		return c.refuse(fmt.Sprintf("%s (heavyq item release %s --force frees it first)", held.Error(), id), "heavyq item claim --next")
	case err != nil:
		fmt.Fprintln(c.stderr, "heavyq:", err)
		return 1
	}
	fmt.Fprintf(c.stdout, "blocked %s: needs: %s\n", id, needs)
	if c.terse {
		fmt.Fprintln(c.stdout, "next: a human must provide it; take other work with: heavyq item claim --next")
	}
	return 0
}

// cleanNeeds validates the need text: the leading "needs:" is optional,
// control characters are refused, the length is bounded.
func cleanNeeds(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) >= 6 && strings.EqualFold(s[:6], "needs:") {
		s = strings.TrimSpace(s[6:])
	}
	switch {
	case s == "":
		return "", errors.New(`say what is needed: heavyq item block <id> "needs: design sign-off"`)
	case len(s) > maxNeedsLen:
		return "", fmt.Errorf("the need is longer than %d bytes", maxNeedsLen)
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", errors.New("the need must be one line of plain text")
		}
	}
	return s, nil
}

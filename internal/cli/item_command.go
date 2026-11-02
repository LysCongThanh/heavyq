package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/lease"
	"github.com/LysCongThanh/heavyq/internal/queue"
)

const itemUsage = `usage:
  heavyq [--config FILE] item ls [--all] [--json]
  heavyq [--config FILE] item claim <id> | --next [--force] [--json]
  heavyq [--config FILE] item release <id> [--force]
  heavyq [--config FILE] item done <id> [--run [--remote WHERE]] [--json]
  heavyq [--config FILE] item block <id> "needs: <what a human must provide>"

Items are the checklist lines of .heavyq/anchor.md (and the read-only extra sources of heavyq.toml [anchor]).
claim leases an item to this lane (TTL 30 min, renewed by every heavyq run in the lane); done refuses until the
item's proof passes on this exact code, then ticks its box; block records what is missing and frees the item.
claim refuses an item with no runnable proof or a proof too long for one lease (--force overrides; --next skips it).
Exit: 0 ok, 1 refused (held, unproven, nothing to claim), 2 usage. See docs/items.md.
`

// itemCommand implements `heavyq item ls|claim|release|done|block`.
func itemCommand(cfg *config.Config, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, itemUsage)
		return ExitUsage
	}
	run := map[string]func(*itemCtx, []string) int{
		"ls": itemLs, "claim": itemClaim, "release": itemRelease, "done": itemDone, "block": itemBlock,
	}[args[0]]
	if run == nil {
		fmt.Fprintf(stderr, "heavyq: unknown item command %q (ls, claim, release, done, block)\n\n%s", args[0], itemUsage)
		return ExitUsage
	}
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	set, err := loadItemSet(cfg, wd)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	c := &itemCtx{itemSet: set, stdout: stdout, stderr: stderr, terse: outputMode(stderr).Terse()}
	c.lane = itemLane(wd)
	c.session = itemSession(c.lane)
	return run(c, args[1:])
}

// itemCtx is one `heavyq item` invocation: the items, who is asking, where to write.
type itemCtx struct {
	*itemSet
	lane, session  string
	stdout, stderr io.Writer
	terse          bool // agent mode: one "next:" line at the end
}

// itemLane is the lane the caller works in: $HEAVYQ_LANE, else the name of the
// git work tree containing dir. The same rule the hook uses for its --lane.
func itemLane(dir string) string {
	if l := os.Getenv("HEAVYQ_LANE"); l != "" {
		return laneOf(l)
	}
	return laneOf(worktreeLane(dir))
}

// laneOf is the form of a lane name that leases and runs use.
func laneOf(name string) string { return queue.SanitizeLane(name) }

// itemSession is the agent session: $HEAVYQ_SESSION, else the one the hook
// recorded for the lane. "" when neither is known (a plain terminal): the
// lane alone is then the identity.
func itemSession(lane string) string {
	if s := os.Getenv("HEAVYQ_SESSION"); history.ValidTag(s) {
		return s
	}
	q, err := queue.Open(QueueDir())
	if err != nil {
		return ""
	}
	return q.ReadSession(lane, time.Now(), history.ValidTag)
}

// ownerFor is who acts on item. Ownership is by lane: when the item's lease
// belongs to this lane, the caller acts as that lease's owner even if the
// session label differs (the hook's session file expires after 10 minutes, and
// two agents in one work tree are the same lane anyway).
func (c *itemCtx) ownerFor(item string) lease.Owner {
	o := lease.Owner{Lane: c.lane, Session: c.session}
	if ls, err := c.store.List(); err == nil {
		for _, l := range ls {
			if l.Item == item && l.Lane == c.lane {
				o.Session = l.Session
			}
		}
	}
	return o
}

// parseItemArgs parses flags and positional arguments in any order.
func parseItemArgs(fs *flag.FlagSet, args []string, stderr io.Writer) ([]string, bool) {
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, itemUsage) }
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, false
		}
		if fs.NArg() == 0 {
			return pos, true
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// oneLine makes untrusted item text safe to print as one line: control
// characters dropped, clipped to n bytes.
func oneLine(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return clip(strings.TrimSpace(s), n)
}

// unknownItem reports an id that is not in the anchor.
func (c *itemCtx) unknownItem(id string) int {
	fmt.Fprintf(c.stderr, "heavyq: no item %q in %s (heavyq item ls --all lists them)\n", id, c.file.Path)
	if c.terse {
		fmt.Fprintln(c.stderr, "next: heavyq item ls")
	}
	return ExitUsage
}

// validID refuses an id the lease store would refuse, with its message.
func (c *itemCtx) validID(id string) bool {
	if err := lease.ValidateItem(id); err != nil {
		fmt.Fprintln(c.stderr, "heavyq:", err)
		return false
	}
	return true
}

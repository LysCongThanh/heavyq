package steer

import (
	"fmt"
	"strconv"
	"strings"
)

// prefix marks text heavyq adds to an agent's context: tool data, facts from
// structured fields, never raw output.
const prefix = "[heavyq · tool data] "

const maxListedBlockers = 3

func itoa(n int) string { return strconv.Itoa(n) }

// blockText is the reason a blocked agent is given: the facts, the exact next
// command, and a way out. In destroy mode the brief rides along every time, so
// the thread survives a long loop.
func blockText(in StopInput, bl []blocker, nth int) string {
	var sb strings.Builder
	first := bl[0]
	switch {
	case first.id == "":
		fmt.Fprintf(&sb, "%s%s.\n", prefix, first.why)
	case len(bl) == 1:
		fmt.Fprintf(&sb, "%sLane %s holds 1 item that is not proven on this tree: %s (%s).\n", prefix, in.Facts.Lane, first.id, first.why)
	default:
		fmt.Fprintf(&sb, "%sLane %s holds %d items that are not proven on this tree: %s.\n", prefix, in.Facts.Lane, len(bl), listBlockers(bl))
	}
	fmt.Fprintf(&sb, "Next: %s\n", first.next)
	if id := escapeItem(in.Facts, bl); id != "" {
		fmt.Fprintf(&sb, "Cannot finish it? heavyq item block %s \"needs: <what>\" or heavyq item release %s.", id, id)
	}
	if in.Mode == Guard {
		fmt.Fprintf(&sb, " Stop blocks left: %d.", max(GuardBlockCap-nth, 0))
	} else {
		sb.WriteString(" (destroy: no cap, the exits are proof, item block, item release or HEAVYQ_STEER=off.)")
	}
	if in.Mode == Destroy {
		if b := BuildBrief(in.Facts, in.State); !b.Empty() {
			sb.WriteString("\n")
			sb.WriteString(b.Render())
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

func listBlockers(bl []blocker) string {
	var parts []string
	for i, b := range bl {
		if i == maxListedBlockers {
			parts = append(parts, fmt.Sprintf("+%d more", len(bl)-i))
			break
		}
		parts = append(parts, b.id+" ("+b.why+")")
	}
	return strings.Join(parts, "; ")
}

// escapeItem is the item whose block/release the text offers: the first
// blocker, or, for a stale gate, the first item the lane holds.
func escapeItem(f Facts, bl []blocker) string {
	if bl[0].id != "" {
		return bl[0].id
	}
	for _, it := range f.Items {
		if it.Lane == f.Lane && it.Lane != "" && (it.State == StateClaimed || it.State == StateProven) {
			return it.ID
		}
	}
	return ""
}

// releaseNote is shown to the user, once the guard cap has let the agent stop.
func releaseNote(bl []blocker) string {
	return fmt.Sprintf("heavyq: stop gate released after %d blocks; still not proven: %s. Prove it: %s. Mute: heavyq steer off",
		GuardBlockCap, blockerIDs(bl), bl[0].next)
}

// claimText is the one-time continuation: the agent said the tests pass, the
// ledger says otherwise on this tree.
func claimText(c FailedCmd, next string) string {
	return fmt.Sprintf("%sThe last message says the tests pass, but the ledger shows `%s` failed on this tree (exit %d).\nNext: %s",
		prefix, oneLine(c.Cmd, 100), c.Exit, next)
}

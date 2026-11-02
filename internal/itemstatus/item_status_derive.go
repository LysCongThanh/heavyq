package itemstatus

import "time"

// ShortCommit is the 7-character form of a commit id shown to people.
func ShortCommit(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

// Derive is the status of one item. The first rule that applies wins:
//
//  1. proven: the proof passes on the evaluated tree.
//  2. believed done but not proven: the box is ticked, or a plan-file item was
//     marked done. stale when the proof passed on an older tree, else
//     claimed_done_unproven.
//  3. blocked: `item block` since the last claim, or a `needs:` in the file.
//  4. claimed: a live lease holds it.
//  5. stale: the proof passed on an older tree.
//  6. open.
//
// An expired lease holds nothing: the item reads as open (and is reclaimable).
func Derive(in Input) Status {
	done := in.Item.Checked || (in.Item.ReadOnly && in.Mark.Done)
	st := Status{ID: in.Item.ID, Reason: in.Proof.Reason, Done: done}
	live := in.Lease != nil && !in.Lease.Stale && !in.Now.After(in.Lease.Expires)
	if live {
		st.Lane, st.Session, st.Expires = in.Lease.Lane, in.Lease.Session, in.Lease.Expires
	}
	switch {
	case in.Proof.Proven:
		st.State, st.Commit, st.At, st.Dirty, st.Reason = Proven, in.Proof.Commit, in.Proof.At, in.Proof.Dirty, ""
		return st
	case done:
		st.Expires = time.Time{}
		if in.Proof.Older != nil {
			st.State, st.Commit, st.At = Stale, in.Proof.Older.Commit, in.Proof.Older.At
		} else {
			st.State = ClaimedDoneUnproven
			st.Reason = unprovenReason(in)
		}
		return st
	}
	if need := blockNeeds(in); need != "" {
		st.State, st.Needs, st.Expires = Blocked, need, time.Time{}
		if in.Mark.Blocked {
			st.Lane, st.At = in.Mark.Lane, in.Mark.At
		}
		return st
	}
	switch {
	case live:
		st.State = Claimed
	case in.Proof.Older != nil:
		st.State, st.Commit, st.At = Stale, in.Proof.Older.Commit, in.Proof.Older.At
	default:
		st.State = Open
	}
	return st
}

// blockNeeds is why the item is blocked, or "" when it is not: a block event
// not yet answered by a claim, else the `needs:` span of the anchor line.
func blockNeeds(in Input) string {
	if in.Mark.Blocked {
		if in.Mark.Needs != "" {
			return in.Mark.Needs
		}
		return "unspecified"
	}
	return in.Item.Needs
}

func unprovenReason(in Input) string {
	switch {
	case !in.Proof.Declared:
		return "ticked, but the item declares no proof"
	case in.Proof.Reason != "":
		return "ticked, but unproven: " + in.Proof.Reason
	}
	return "ticked, but its proof has not passed on this code"
}

// Claimable reports whether `claim <id>` may take the item, and whether
// `claim --next` may pick it. Open and stale items are up for grabs; an item
// blocked by an event can be claimed by name (that is how it is unblocked) but
// is never picked by --next; a `needs:` in the file blocks until the file
// changes. Everything else (claimed, proven, done) is not claimable; an item
// the caller's lane already holds is handled by the lease store.
func Claimable(st Status, fileNeeds bool) (byName, next bool) {
	if st.Done {
		return false, false // finished work is not handed out again, even when its proof went stale
	}
	switch st.State {
	case Open, Stale:
		return true, true
	case Blocked:
		return !fileNeeds, false
	}
	return false, false
}

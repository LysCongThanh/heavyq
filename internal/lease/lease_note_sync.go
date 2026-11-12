package lease

// NoteSync appends the OpSync event of item: its box in the anchor file has
// been ticked by `heavyq item sync`, so the box carries the done from now on
// (a later hand edit of it counts again) and the done event no longer does.
// It touches no lease. Call it inside WithLock, right after the box is ticked,
// so a claim cannot slip in between the tick and the event.
func (s *Store) NoteSync(item string, owner Owner, commit string) error {
	if err := ValidateItem(item); err != nil {
		return err
	}
	if err := owner.validateIdentity(); err != nil {
		return err
	}
	return s.appendEvent(Event{At: s.now(), Op: OpSync, Item: item, Lane: owner.Lane, Session: owner.Session, Commit: commit})
}

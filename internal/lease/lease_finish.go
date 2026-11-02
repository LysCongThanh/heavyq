package lease

import (
	"errors"
	"fmt"
	"os"
)

// WithLock runs fn holding the store's directory lock, the one every claim,
// renew, release and Finish takes. Callers that must change something in step
// with the leases (ticking a checkbox) do it here.
func (s *Store) WithLock(fn func() error) error { return s.withLock(fn) }

// Finish ends owner's work on item for good: ev.Op must be OpDone or OpBlock.
// Under one lock it checks that no other live owner holds the item
// (*HeldError otherwise; a stale or missing lease is fine), runs fn if given,
// appends ev (with Item, Lane, Session and At filled in) and removes the lease.
//
// The order is fn, event, release: when fn or the event append fails nothing
// is released, so the caller can simply try again. A failed append is returned
// wrapping ErrEventLog.
func (s *Store) Finish(item string, owner Owner, ev Event, fn func() error) error {
	if err := ValidateItem(item); err != nil {
		return err
	}
	if err := owner.validateIdentity(); err != nil {
		return err
	}
	if ev.Op != OpDone && ev.Op != OpBlock {
		return fmt.Errorf("lease: Finish takes %q or %q, got %q", OpDone, OpBlock, ev.Op)
	}
	return s.withLock(func() error {
		cur, err := s.read(item)
		switch {
		case err == nil && !cur.Owner().Same(owner) && s.staleReason(cur, s.now()) == "":
			return &HeldError{Holder: cur}
		case err != nil && !errors.Is(err, ErrNotFound) && !errors.Is(err, errCorrupt):
			return err
		}
		if fn != nil {
			if err := fn(); err != nil {
				return err
			}
		}
		ev.At, ev.Item, ev.Lane, ev.Session = s.now(), item, owner.Lane, owner.Session
		if err := s.appendEvent(ev); err != nil {
			return err
		}
		if err := os.Remove(s.path(item)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove lease %s: %w", item, err)
		}
		return nil
	})
}

package lease

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Claim leases item for owner.
//
//   - no lease, or a corrupt one: granted ("claim").
//   - held by the same owner (same lane and session): renewed, with pid and
//     start refreshed when owner has a pid ("renew"); Acquired is kept.
//   - held by another owner whose lease is expired or whose explicit pid is dead:
//     reclaimed ("reclaim").
//   - held by another live, unexpired owner: *HeldError (errors.Is ErrHeld).
//
// If only the event log append fails, the lease is granted and returned
// together with an error wrapping ErrEventLog.
func (s *Store) Claim(item string, owner Owner) (Lease, error) {
	if err := ValidateItem(item); err != nil {
		return Lease{}, err
	}
	if err := owner.validate(); err != nil {
		return Lease{}, err
	}
	return s.ClaimGuarded(Request{Item: item}, owner, nil)
}

// ClaimNext claims the first candidate, in order, that is claimable (free,
// stale, or already held by owner) and reports whether one was found. All
// candidates are validated first; the scan runs under one lock, so
// concurrent callers get distinct items.
func (s *Store) ClaimNext(candidates []string, owner Owner) (Lease, bool, error) {
	reqs := make([]Request, len(candidates))
	for i, c := range candidates {
		reqs[i] = Request{Item: c}
	}
	l, found, _, err := s.ClaimNextGuarded(reqs, owner, nil)
	return l, found, err
}

// claimLocked is Claim's body; the caller holds the directory lock. admit, when
// not nil, may refuse a lease that is not a renewal (see Admit).
func (s *Store) claimLocked(req Request, owner Owner, admit Admit) (Lease, error) {
	item, now := req.Item, s.now().UTC()
	l := Lease{Item: item, Lane: owner.Lane, Session: owner.Session, Pid: owner.Pid,
		Start: owner.Start, Acquired: now, Renewed: now, Expires: now.Add(s.TTL), Paths: req.Paths}
	ev := Event{At: now, Op: opClaim, Item: item, Lane: owner.Lane, Session: owner.Session}

	cur, err := s.read(item)
	switch {
	case errors.Is(err, ErrNotFound):
	case errors.Is(err, errCorrupt):
		ev.Op, ev.Reason = opReclaim, "corrupt"
	case err != nil:
		return Lease{}, err
	case cur.Owner().Same(owner):
		l.Acquired = cur.Acquired
		if owner.Pid == 0 { // no new evidence: keep what the lease has
			l.Pid, l.Start = cur.Pid, cur.Start
		}
		ev.Op = opRenew
	default:
		reason := s.staleReason(cur, now)
		if reason == "" {
			return Lease{}, &HeldError{Holder: cur}
		}
		ev.Op, ev.Reason, ev.PrevLane, ev.PrevSession = opReclaim, reason, cur.Lane, cur.Session
	}
	if ev.Op != opRenew && admit != nil { // an item already in hand is never judged again
		if err := s.admit(req, admit); err != nil {
			return Lease{}, err
		}
	}
	if err := s.write(l); err != nil {
		return Lease{}, err
	}
	return l, s.appendEvent(ev)
}

// Renew extends the lease of item by a full TTL and refreshes pid/start.
// Only the holder (same lane and session) may renew, even when the lease has
// already expired, provided nobody reclaimed it meanwhile. Other owners get
// *HeldError; a missing lease gives ErrNotFound.
func (s *Store) Renew(item string, owner Owner) (Lease, error) {
	if err := ValidateItem(item); err != nil {
		return Lease{}, err
	}
	if err := owner.validate(); err != nil {
		return Lease{}, err
	}
	var out Lease
	err := s.withLock(func() error {
		cur, err := s.read(item)
		if errors.Is(err, errCorrupt) {
			return fmt.Errorf("%w: %v", ErrNotFound, err)
		}
		if err != nil {
			return err
		}
		if !cur.Owner().Same(owner) {
			cur.Stale = s.staleReason(cur, s.now()) != ""
			return &HeldError{Holder: cur}
		}
		now := s.now().UTC()
		if owner.Pid > 0 {
			cur.Pid, cur.Start = owner.Pid, owner.Start
		}
		cur.Renewed, cur.Expires = now, now.Add(s.TTL)
		if err := s.write(cur); err != nil {
			return err
		}
		out = cur
		return s.appendEvent(Event{At: now, Op: opRenew, Item: item, Lane: owner.Lane, Session: owner.Session})
	})
	return out, err
}

// Release drops the lease of item. Only the holder may release, unless force
// is set (then owner only attributes the event and needs lane only). A
// missing lease gives ErrNotFound; a foreign lease without force gives
// *HeldError.
func (s *Store) Release(item string, owner Owner, force bool) error {
	if err := ValidateItem(item); err != nil {
		return err
	}
	if err := owner.validateIdentity(); err != nil {
		return err
	}
	return s.withLock(func() error {
		cur, err := s.read(item)
		corrupt := errors.Is(err, errCorrupt)
		if err != nil && !corrupt {
			return err
		}
		forced := false
		if corrupt || !cur.Owner().Same(owner) {
			if !force {
				if corrupt {
					return fmt.Errorf("%w: %v", ErrNotFound, err)
				}
				cur.Stale = s.staleReason(cur, s.now()) != ""
				return &HeldError{Holder: cur}
			}
			forced = true
		}
		if err := os.Remove(s.path(item)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove lease %s: %w", item, err)
		}
		return s.appendEvent(Event{At: s.now(), Op: opRelease, Item: item,
			Lane: owner.Lane, Session: owner.Session, Forced: forced})
	})
}

// List returns every lease sorted by item, with Stale set for leases that are
// expired or whose explicit owner pid is gone. It takes no lock (files are replaced
// atomically). Unreadable or corrupt files are skipped; Claim overwrites them.
func (s *Store) List() ([]Lease, error) {
	ents, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("list leases: %w", err)
	}
	now := s.now()
	var out []Lease
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
			continue
		}
		item := strings.ReplaceAll(strings.TrimSuffix(name, ".json"), "+", "/")
		if ValidateItem(item) != nil {
			continue
		}
		l, err := s.read(item)
		if err != nil {
			continue
		}
		l.Stale = s.staleReason(l, now) != ""
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Item < out[j].Item })
	return out, nil
}

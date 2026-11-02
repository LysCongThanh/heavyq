package lease

import (
	"errors"
	"fmt"
)

// Request is one item to claim, with what the claim says about it.
type Request struct {
	Item  string
	Paths []string // stored on the lease; what later claims are checked against
}

// Admit is a claim policy. It runs under the store lock, so what it sees is
// what the grant is decided on: live is every live lease of another item (any
// lane), read after this item's own state was settled. It is not run for a
// renewal, because an item already in hand is never judged again. A non-nil
// error refuses the lease; it comes back wrapped in a *RefusedError.
type Admit func(req Request, live []Lease) error

// RefusedError is an Admit refusal of one item.
type RefusedError struct {
	Item string
	Err  error
}

func (e *RefusedError) Error() string { return e.Err.Error() }
func (e *RefusedError) Unwrap() error { return e.Err }

// ClaimGuarded is Claim with a policy: under the lock, after the holder
// checks, admit may refuse. A nil admit grants like Claim.
func (s *Store) ClaimGuarded(req Request, owner Owner, admit Admit) (Lease, error) {
	if err := ValidateItem(req.Item); err != nil {
		return Lease{}, err
	}
	if err := owner.validate(); err != nil {
		return Lease{}, err
	}
	var out Lease
	err := s.withLock(func() (err error) {
		out, err = s.claimLocked(req, owner, admit)
		return err
	})
	return out, err
}

// ClaimNextGuarded is ClaimNext with a policy: the first request that is free
// (or already owner's) and that admit lets through is granted. Requests admit
// refuses are passed over and returned in refused, by item. found is false when
// nothing could be granted.
func (s *Store) ClaimNextGuarded(reqs []Request, owner Owner, admit Admit) (l Lease, found bool, refused map[string]error, err error) {
	for _, r := range reqs {
		if err := ValidateItem(r.Item); err != nil {
			return Lease{}, false, nil, err
		}
	}
	if err := owner.validate(); err != nil {
		return Lease{}, false, nil, err
	}
	refused = map[string]error{}
	err = s.withLock(func() error {
		for _, r := range reqs {
			got, err := s.claimLocked(r, owner, admit)
			var ref *RefusedError
			switch {
			case errors.Is(err, ErrHeld):
				continue
			case errors.As(err, &ref):
				refused[r.Item] = ref.Err
				continue
			}
			l, found = got, err == nil || errors.Is(err, ErrEventLog)
			return err
		}
		return nil
	})
	return l, found, refused, err
}

// admit runs the policy against the live leases of the other items.
func (s *Store) admit(req Request, admit Admit) error {
	all, err := s.List()
	if err != nil {
		return err
	}
	live := all[:0:0]
	for _, o := range all {
		if o.Item != req.Item && !o.Stale {
			live = append(live, o)
		}
	}
	if err := admit(req, live); err != nil {
		return &RefusedError{Item: req.Item, Err: fmt.Errorf("%w", err)}
	}
	return nil
}

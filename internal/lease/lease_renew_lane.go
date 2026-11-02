package lease

import "fmt"

// RenewLane extends every lease held by lane by a full TTL and returns how
// many it renewed. Any heavyq run in a lane proves the lane is alive, so the
// run calls this when it ends. It is cheap when the lane holds nothing (no
// lock, one directory read) and writes no event: renewals by activity would
// drown the log.
func (s *Store) RenewLane(lane string) (int, error) {
	ls, err := s.List()
	if err != nil {
		return 0, err
	}
	mine := false
	for _, l := range ls {
		mine = mine || l.Lane == lane
	}
	if !mine {
		return 0, nil
	}
	n := 0
	err = s.withLock(func() error {
		now := s.now().UTC()
		for _, l := range ls {
			if l.Lane != lane {
				continue
			}
			cur, err := s.read(l.Item) // re-read: it may have been released or taken over
			if err != nil || cur.Lane != lane {
				continue
			}
			cur.Renewed, cur.Expires = now, now.Add(s.TTL)
			if err := s.write(cur); err != nil {
				return fmt.Errorf("renew lane %s: %w", lane, err)
			}
			n++
		}
		return nil
	})
	return n, err
}

package lease

import (
	"errors"
	"fmt"
	"os"
	"time"
	"unicode"

	"github.com/LysCongThanh/heavyq/internal/procinfo"
)

// DefaultTTL is how long a lease lives without a renewal.
const DefaultTTL = 30 * time.Minute

// maxOwnerField bounds lane and session so a lease file stays small.
const maxOwnerField = 256

// Sentinel errors. Use errors.Is; ErrHeld is usually delivered as *HeldError.
var (
	// ErrHeld: a live, unexpired lease of another owner exists.
	ErrHeld = errors.New("lease held by another owner")
	// ErrNotFound: no lease exists for the item.
	ErrNotFound = errors.New("no lease for item")
	// ErrEventLog: the lease operation succeeded but events.jsonl could not
	// be appended. The returned Lease is valid.
	ErrEventLog = errors.New("lease event log append failed")
	// ErrLockTimeout: the store lock stayed busy past Store.LockTimeout.
	ErrLockTimeout = errors.New("timed out waiting for lease store lock")

	errCorrupt = errors.New("corrupt lease file")
)

// Lease is the claim of one item by one owner. Only the fields up to Expires
// are persisted; Stale is computed by List.
type Lease struct {
	Item     string    `json:"item"`
	Lane     string    `json:"lane"`
	Session  string    `json:"session"`
	Pid      int       `json:"pid"`   // optional owner process; 0 = none
	Start    string    `json:"start"` // its start token (procinfo.StartTime); empty when Pid is 0
	Acquired time.Time `json:"acquired"`
	Renewed  time.Time `json:"renewed"`
	Expires  time.Time `json:"expires"`
	// Stale is set by List: the lease is expired, or its explicit owner pid is gone.
	Stale bool `json:"stale,omitempty"`
}

// Owner returns the identity that holds the lease.
func (l Lease) Owner() Owner {
	return Owner{Lane: l.Lane, Session: l.Session, Pid: l.Pid, Start: l.Start}
}

// Owner identifies the claimant: lane + session, plus optional process
// evidence (pid and start token).
//
// Liveness is the TTL: a lease is stale once now is past Expires, and
// renewals (any Claim or Renew by the same owner) push Expires out. Claimers
// are usually short-lived CLI calls and hooks whose pid dies right after the
// claim, so Pid == 0 (the default) means no pid check at all.
//
// A caller that has a long-lived process to vouch for the session (a wrapper
// such as `heavyq exec codex`) may set Pid and Start (see NewOwner,
// CurrentProcess). Then a dead pid, or a reused pid whose start token no
// longer matches, makes the lease stale before the TTL runs out.
//
// Two owners are the same when lane and session match (Same); pid is never
// part of identity. A repeated claim by the same session renews its lease,
// and refreshes pid/start when the caller supplies a pid (a caller without
// one keeps the evidence already stored).
type Owner struct {
	Lane    string
	Session string
	Pid     int
	Start   string
}

// NewOwner builds an Owner. pid <= 0 means no process evidence (TTL-only
// liveness). For a pid > 0 the start token is read from /proc, which fails
// when the process does not exist.
func NewOwner(lane, session string, pid int) (Owner, error) {
	if pid <= 0 {
		return Owner{Lane: lane, Session: session}, nil
	}
	start, err := procinfo.Default.StartTime(pid)
	if err != nil {
		return Owner{}, fmt.Errorf("start token of pid %d: %w", pid, err)
	}
	return Owner{Lane: lane, Session: session, Pid: pid, Start: start}, nil
}

// CurrentProcess builds an Owner tied to the calling process: the lease turns
// stale early if this process exits. Use it only when this process lives as
// long as the work it claims.
func CurrentProcess(lane, session string) (Owner, error) {
	return NewOwner(lane, session, os.Getpid())
}

// Same reports whether o and p are the same agent session (lane + session).
func (o Owner) Same(p Owner) bool { return o.Lane == p.Lane && o.Session == p.Session }

// validateIdentity checks lane and session (all that Release needs).
func (o Owner) validateIdentity() error {
	if o.Lane == "" {
		return errors.New("lease owner: lane is required")
	}
	for name, v := range map[string]string{"lane": o.Lane, "session": o.Session} {
		if len(v) > maxOwnerField {
			return fmt.Errorf("lease owner: %s longer than %d bytes", name, maxOwnerField)
		}
		for _, r := range v {
			if unicode.IsControl(r) {
				return fmt.Errorf("lease owner: %s contains a control character", name)
			}
		}
	}
	return nil
}

// validate additionally checks the optional process evidence: a pid needs
// its start token, and no start token without a pid.
func (o Owner) validate() error {
	if err := o.validateIdentity(); err != nil {
		return err
	}
	if o.Pid < 0 || (o.Pid > 0) != (o.Start != "") {
		return errors.New("lease owner: pid and start token go together (see NewOwner); pid 0 means none")
	}
	return nil
}

// HeldError reports the live holder that blocked an operation.
type HeldError struct {
	Holder Lease
}

func (e *HeldError) Error() string {
	return fmt.Sprintf("item %q is held by lane %q session %q until %s",
		e.Holder.Item, e.Holder.Lane, e.Holder.Session, e.Holder.Expires.Format(time.RFC3339))
}

// Is makes errors.Is(err, ErrHeld) true for *HeldError.
func (e *HeldError) Is(target error) bool { return target == ErrHeld }

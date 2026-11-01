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
	Pid      int       `json:"pid"`
	Start    string    `json:"start"` // process start token, see procinfo.StartTime
	Acquired time.Time `json:"acquired"`
	Renewed  time.Time `json:"renewed"`
	Expires  time.Time `json:"expires"`
	// Stale is set by List: the lease is expired, or its owner process is gone.
	Stale bool `json:"stale,omitempty"`
}

// Owner returns the identity that holds the lease.
func (l Lease) Owner() Owner {
	return Owner{Lane: l.Lane, Session: l.Session, Pid: l.Pid, Start: l.Start}
}

// Owner identifies the claimant: lane + session + the process (pid and start
// token) that proves it is still around.
//
// Two owners are the same owner when lane and session match (Same). The pid
// may differ between heavyq invocations of one agent session, so a repeated
// claim by the same session renews the lease and refreshes pid/start.
// Pid should be a process that lives as long as the session (the agent), not
// a short-lived CLI invocation, or its lease turns stale as soon as it exits.
type Owner struct {
	Lane    string
	Session string
	Pid     int
	Start   string
}

// NewOwner builds an Owner for pid (pid <= 0 means the current process),
// reading its start token from /proc.
func NewOwner(lane, session string, pid int) (Owner, error) {
	if pid <= 0 {
		pid = os.Getpid()
	}
	start, err := procinfo.Default.StartTime(pid)
	if err != nil {
		return Owner{}, fmt.Errorf("start token of pid %d: %w", pid, err)
	}
	return Owner{Lane: lane, Session: session, Pid: pid, Start: start}, nil
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

// validate additionally requires a process the lease can be tied to.
func (o Owner) validate() error {
	if err := o.validateIdentity(); err != nil {
		return err
	}
	if o.Pid <= 0 || o.Start == "" {
		return errors.New("lease owner: pid and start token are required (see NewOwner)")
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

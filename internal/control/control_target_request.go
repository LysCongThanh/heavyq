package control

import (
	"errors"
	"os"
	"strings"
	"time"
)

// A target request (`heavyq retarget`, the dashboard) asks the owner of a
// waiting ticket to wait for other machines, and the owner answers it:
//
//	<key>.target       the new placement target: local, auto, a remote name or a list "a,b"
//	<key>.target-done  the answer, once the owner read the request: empty when it
//	                   applied the move, else why it refused (the ticket keeps its machines)
//
// The requester checks what it can before filing (internal/cli,
// retarget_rules.go); the owner checks again with its own configuration and
// what only it knows, such as whether this machine may run its code.
//
// An answer outlives its ticket (Forget keeps it): a moved ticket may get its
// slot, run and end before the requester looks. Answers nobody read (the
// dashboard does not wait for one) are swept by later requests after answerTTL.

// answerTTL is how long an unread answer is kept.
const answerTTL = 10 * time.Minute

// validTarget reports whether t has the shape of a placement target: 1 to 32
// machine names (machineRe) separated by commas. Whether they are configured is
// the caller's check.
func validTarget(t string) bool {
	ms := strings.Split(t, ",")
	if len(ms) > 32 {
		return false
	}
	for _, m := range ms {
		if !machineRe.MatchString(m) {
			return false
		}
	}
	return true
}

// ErrBadTarget rejects a target that is not machine names separated by commas.
var ErrBadTarget = errors.New("invalid placement target (want local, auto, a remote name or a list a,b)")

// RequestTarget asks the owner of a waiting ticket to wait for the machines of
// target instead. An answer to an earlier request is dropped first, so the next
// answer is to this one.
func (m Mailbox) RequestTarget(ticket, target string) error {
	if !ticketRe.MatchString(ticket) {
		return ErrBadName
	}
	if !validTarget(target) {
		return ErrBadTarget
	}
	if err := os.Remove(m.path(key(ticket) + ".target-done")); err != nil && !os.IsNotExist(err) {
		return err
	}
	m.sweepAnswers(time.Now())
	return m.put(key(ticket)+".target", target)
}

// sweepAnswers removes the answers older than answerTTL (best effort).
func (m Mailbox) sweepAnswers(now time.Time) {
	ents, err := os.ReadDir(m.Dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".target-done") {
			continue
		}
		if fi, err := e.Info(); err == nil && now.Sub(fi.ModTime()) > answerTTL {
			_ = os.Remove(m.path(e.Name()))
		}
	}
}

// TakeTarget returns and consumes a pending target request for ticket. A
// request of the wrong shape is consumed and ignored.
func (m Mailbox) TakeTarget(ticket string) (string, bool) {
	if !ticketRe.MatchString(ticket) {
		return "", false
	}
	b, ok := m.take(key(ticket) + ".target")
	t := strings.TrimSpace(b)
	return t, ok && validTarget(t)
}

// AnswerTarget records the owner's answer to the request it took: refusal is
// "" when it applied the move.
func (m Mailbox) AnswerTarget(ticket, refusal string) error {
	if !ticketRe.MatchString(ticket) {
		return ErrBadName
	}
	return m.put(key(ticket)+".target-done", refusal)
}

// TargetAnswer returns and consumes the owner's answer to the last request:
// refusal "" means the move was applied; ok is false while there is no answer.
func (m Mailbox) TargetAnswer(ticket string) (refusal string, ok bool) {
	if !ticketRe.MatchString(ticket) {
		return "", false
	}
	b, ok := m.take(key(ticket) + ".target-done")
	return strings.TrimSpace(b), ok
}

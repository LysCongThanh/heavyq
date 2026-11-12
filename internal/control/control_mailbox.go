// Package control carries operator requests (cancel a ticket, change its
// priority or its machines, pause a machine) from the dashboard or the CLI to
// the heavyq processes that own the tickets.
//
// heavyq has no daemon, so requests are files in <queue dir>/ctl that the
// ticket's own process picks up on its next poll:
//
//	<key>.cancel      stop the ticket (waiting: leave the queue; running: SIGTERM)
//	<key>.prio        new priority 1-9 (waiting tickets only)
//	<key>.target      new placement target (waiting tickets only), answered in
//	                  <key>.target-done (control_target_request.go)
//
// <key> is the ticket name without its leading priority digit, so a request
// still reaches a ticket whose priority changed in the meantime.
//
//	pause-<machine>   no new admissions on machine; running jobs continue
package control

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
)

// Mailbox is the control directory.
type Mailbox struct{ Dir string }

var (
	ticketRe  = regexp.MustCompile(`^[1-9]-[0-9]{20}-[0-9]+-[A-Za-z0-9_.-]+$`)
	machineRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
)

// ErrBadName rejects ids that could escape the control directory.
var ErrBadName = errors.New("invalid ticket or machine name")

// key drops the "<prio>-" prefix of a (validated) ticket name.
func key(ticket string) string { return ticket[2:] }

func (m Mailbox) path(name string) string { return filepath.Join(m.Dir, name) }

// put stores a request atomically. Every writer has a temporary file of its
// own: the dashboard (whose handlers run concurrently) and the CLI may write
// to one ticket at the same moment, and a shared name would let one writer
// rename the other's half-written file away.
func (m Mailbox) put(name, content string) error {
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(m.Dir, "."+name+".*.tmp") // mode 0600
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.WriteString(content)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, m.path(name))
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// claims makes the names TakePrio moves a request to unique per call.
var claims atomic.Uint64

func (m Mailbox) exists(name string) bool {
	_, err := os.Stat(m.path(name))
	return err == nil
}

// RequestCancel asks the owner of ticket to stop it.
func (m Mailbox) RequestCancel(ticket string) error {
	if !ticketRe.MatchString(ticket) {
		return ErrBadName
	}
	return m.put(key(ticket)+".cancel", "")
}

// CancelRequested reports whether ticket was asked to stop.
func (m Mailbox) CancelRequested(ticket string) bool {
	return ticketRe.MatchString(ticket) && m.exists(key(ticket)+".cancel")
}

// RequestPrio asks the owner of a waiting ticket to move to priority prio.
func (m Mailbox) RequestPrio(ticket string, prio int) error {
	if !ticketRe.MatchString(ticket) {
		return ErrBadName
	}
	if prio < 1 || prio > 9 {
		return fmt.Errorf("priority must be 1-9, got %d", prio)
	}
	return m.put(key(ticket)+".prio", strconv.Itoa(prio))
}

// TakePrio returns and consumes a pending priority request for ticket.
//
// It claims the request by renaming it to a name of its own before reading
// it, so a newer request written meanwhile stays in place for the next poll
// instead of being deleted unseen.
func (m Mailbox) TakePrio(ticket string) (int, bool) {
	if !ticketRe.MatchString(ticket) {
		return 0, false
	}
	name := key(ticket) + ".prio"
	claimed := m.path(fmt.Sprintf(".%s.%d-%d.taken", name, os.Getpid(), claims.Add(1)))
	if err := os.Rename(m.path(name), claimed); err != nil {
		return 0, false
	}
	b, err := os.ReadFile(claimed)
	_ = os.Remove(claimed)
	if err != nil {
		return 0, false
	}
	p, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || p < 1 || p > 9 {
		return 0, false
	}
	return p, true
}

// Forget removes every request addressed to ticket (called when it ends). An
// answer to a target request stays for its requester (control_target_request.go).
func (m Mailbox) Forget(ticket string) {
	if !ticketRe.MatchString(ticket) {
		return
	}
	for _, suffix := range []string{".cancel", ".prio", ".target"} {
		_ = os.Remove(m.path(key(ticket) + suffix))
	}
}

// SetPaused pauses or resumes admissions on machine.
func (m Mailbox) SetPaused(machine string, paused bool) error {
	if !machineRe.MatchString(machine) {
		return ErrBadName
	}
	if !paused {
		if err := os.Remove(m.path("pause-" + machine)); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return m.put("pause-"+machine, "")
}

// Paused reports whether admissions on machine are paused.
func (m Mailbox) Paused(machine string) bool {
	return machineRe.MatchString(machine) && m.exists("pause-"+machine)
}

// ValidTicket reports whether id has the shape of a ticket name.
func ValidTicket(id string) bool { return ticketRe.MatchString(id) }

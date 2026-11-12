package queue

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Dir is a queue directory.
type Dir struct{ Path string }

// Open creates (if needed) and returns the queue directory. It refuses a
// directory that is a symlink or owned by another user (a pre-created
// /tmp/heavyq-<uid> could otherwise be used to inject tickets), and tightens
// group/world write permission on our own directory.
func Open(path string) (*Dir, error) {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return nil, fmt.Errorf("queue dir %s: %w", path, err)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("queue dir %s: %w", path, err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("queue dir %s is not a directory (symlink?)", path)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return nil, fmt.Errorf("queue dir %s is owned by uid %d, not us; refusing to use it", path, st.Uid)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		if err := os.Chmod(path, 0o700); err != nil {
			return nil, fmt.Errorf("queue dir %s is group/world writable: %w", path, err)
		}
	}
	return &Dir{Path: path}, nil
}

// writeAtomic writes data to name via a temp file + rename.
func (d *Dir) writeAtomic(name string, data []byte) error {
	tmp := filepath.Join(d.Path, "."+name+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(d.Path, name)); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Add enqueues t and sets t.Name.
func (d *Dir) Add(t *Ticket) error {
	t.Name = t.fileName(d.enqueueTime(time.Now()))
	b, err := t.encode()
	if err != nil {
		return err
	}
	if err := d.writeAtomic(t.Name, b); err != nil {
		return fmt.Errorf("write ticket: %w", err)
	}
	return nil
}

// Claim marks t as running on machine/slot.
func (d *Dir) Claim(t *Ticket, machine, slot string) error {
	t.Machine, t.Slot, t.ClaimedAt = machine, slot, time.Now()
	b, err := t.encode()
	if err != nil {
		return err
	}
	if err := d.writeAtomic(t.Name+runSuffix, b); err != nil {
		return fmt.Errorf("claim ticket: %w", err)
	}
	t.Running = true
	_ = os.Remove(filepath.Join(d.Path, t.Name))
	return nil
}

// Remove deletes both forms of t. Safe to call repeatedly.
func (d *Dir) Remove(t *Ticket) {
	if t == nil || t.Name == "" {
		return
	}
	_ = os.Remove(filepath.Join(d.Path, t.Name))
	_ = os.Remove(filepath.Join(d.Path, t.Name+runSuffix))
}

// Exists reports whether the waiting or running file of t is present.
func (d *Dir) Exists(t *Ticket) bool {
	for _, n := range []string{t.Name, t.Name + runSuffix} {
		if _, err := os.Stat(filepath.Join(d.Path, n)); err == nil {
			return true
		}
	}
	return false
}

// Snapshot is the queue content at one instant.
type Snapshot struct {
	Waiting []*Ticket // queue order
	Running []*Ticket
}

// List reads every ticket. Unparseable files are ignored (and pruned by
// Prune once they are old enough to not be in-flight writes).
func (d *Dir) List() (Snapshot, error) {
	ents, err := os.ReadDir(d.Path)
	if err != nil {
		return Snapshot{}, err
	}
	var s Snapshot
	running := map[string]bool{}
	var names []string
	for _, e := range ents {
		n := e.Name()
		if !ticketName.MatchString(strings.TrimSuffix(n, runSuffix)) {
			continue
		}
		if strings.HasSuffix(n, runSuffix) {
			running[strings.TrimSuffix(n, runSuffix)] = true
		}
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if !strings.HasSuffix(n, runSuffix) && running[n] {
			continue // mid-claim: the .run file is authoritative
		}
		b, err := os.ReadFile(filepath.Join(d.Path, n))
		if err != nil {
			continue
		}
		t, err := decodeTicket(n, b)
		if err != nil {
			continue
		}
		if t.Running {
			s.Running = append(s.Running, t)
		} else {
			s.Waiting = append(s.Waiting, t)
		}
	}
	return s, nil
}

// Reprio moves waiting ticket t to priority prio. The ticket keeps its
// enqueue time, so among equal priorities it keeps its FIFO place. Call it
// under the queue mutex.
func (d *Dir) Reprio(t *Ticket, prio int) error {
	if t.Running || prio == t.Prio || prio < 1 || prio > 9 {
		return nil
	}
	old := t.Name
	t.Prio = prio
	t.Name = fmt.Sprintf("%d%s", prio, old[1:])
	b, err := t.encode()
	if err == nil {
		err = d.writeAtomic(t.Name, b)
	}
	if err != nil {
		t.Name, t.Prio = old, int(old[0]-'0')
		return fmt.Errorf("reprio ticket: %w", err)
	}
	_ = os.Remove(filepath.Join(d.Path, old))
	return nil
}

// Rewrite stores the current fields of waiting ticket t (under the mutex).
func (d *Dir) Rewrite(t *Ticket) error {
	if t.Running {
		return nil
	}
	b, err := t.encode()
	if err != nil {
		return err
	}
	return d.writeAtomic(t.Name, b)
}

// Find returns the waiting or running ticket called name. A name whose
// priority digit is outdated (the ticket was re-prioritised) still matches.
func (d *Dir) Find(name string) (*Ticket, bool) {
	s, err := d.List()
	if err != nil {
		return nil, false
	}
	for _, t := range append(s.Waiting, s.Running...) {
		if t.Name == name || (len(name) > 2 && len(t.Name) > 2 && t.Name[1:] == name[1:]) {
			return t, true
		}
	}
	return nil, false
}

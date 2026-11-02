package stance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/LysCongThanh/heavyq/internal/slots"
)

// Store is the stance state of a repository: one small JSON file per lane and
// an append-only events log, under the git common dir so that every work tree
// of the repository sees them, and never committed:
//
//	<git-common-dir>/heavyq/stance/lanes/<lane>.json   State (0600)
//	<git-common-dir>/heavyq/stance/events.jsonl        Events (0600)
//	<git-common-dir>/heavyq/stance/.lock               directory flock for changes
//
// State files are replaced atomically, so readers need no lock.
type Store struct {
	dir string
	// LockTimeout bounds the wait for the flock (default 2 s).
	LockTimeout time.Duration
}

var laneNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// OpenStore returns the store under commonDir (gitutil.CommonDir), which must
// exist: it never invents a repository.
func OpenStore(commonDir string) (*Store, error) {
	if fi, err := os.Stat(commonDir); commonDir == "" || err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("stance: git common dir %q is not a directory", commonDir)
	}
	return &Store{dir: filepath.Join(commonDir, "heavyq", "stance"), LockTimeout: 2 * time.Second}, nil
}

func (s *Store) laneFile(lane string) string { return filepath.Join(s.dir, "lanes", lane+".json") }

// Load returns the state of lane; a lane never seen has the zero state.
func (s *Store) Load(lane string) (State, error) {
	st := State{V: stateSchema, Lane: lane}
	if !laneNameRe.MatchString(lane) {
		return st, fmt.Errorf("stance: invalid lane name %q", lane)
	}
	data, err := os.ReadFile(s.laneFile(lane))
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("stance: read lane state: %w", err)
	}
	if err := json.Unmarshal(data, &st); err != nil || st.Lane != lane {
		// A damaged file must not wedge the lane: start again, the events log keeps the history.
		return State{V: stateSchema, Lane: lane}, nil
	}
	return st, nil
}

// Update runs fn on the state of lane under the store lock and saves the
// state it leaves, then appends the events it returns. An error from fn saves
// nothing, and neither does a call that returns no events. The events are written after the state: a crash in between loses a
// log line, never a state change.
func (s *Store) Update(lane string, fn func(st *State) ([]Event, error)) error {
	if !laneNameRe.MatchString(lane) {
		return fmt.Errorf("stance: invalid lane name %q", lane)
	}
	return s.withLock(func() error {
		st, err := s.Load(lane)
		if err != nil {
			return err
		}
		events, err := fn(&st)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			return nil // nothing happened: leave the file alone
		}
		if err := s.save(st); err != nil {
			return err
		}
		return s.appendEvents(events)
	})
}

func (s *Store) save(st State) error {
	st.V = stateSchema
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	path := s.laneFile(st.Lane)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("stance: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("stance: %w", err)
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("stance: write lane state: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("stance: %w", err)
	}
	return nil
}

func (s *Store) appendEvents(events []Event) error {
	if len(events) == 0 {
		return nil
	}
	var buf bytes.Buffer
	for _, e := range events {
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		buf.Write(append(b, '\n'))
	}
	f, err := os.OpenFile(filepath.Join(s.dir, "events.jsonl"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("stance: events log: %w", err)
	}
	_, werr := f.Write(buf.Bytes())
	return errors.Join(werr, f.Close())
}

// maxEventsRead bounds what Events reads: the newest part of the log.
const maxEventsRead = 4 << 20

// Events returns the logged events, oldest first (the newest 4 MiB of the
// log). Lines that are not events are skipped.
func (s *Store) Events() ([]Event, error) {
	f, err := os.Open(filepath.Join(s.dir, "events.jsonl"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stance: events log: %w", err)
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() > maxEventsRead {
		if _, err := f.Seek(fi.Size()-maxEventsRead, io.SeekStart); err != nil {
			return nil, err
		}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("stance: events log: %w", err)
	}
	var out []Event
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		var e Event
		if len(line) > 0 && json.Unmarshal(line, &e) == nil && e.Op != "" {
			out = append(out, e)
		}
	}
	return out, nil
}

// withLock runs fn holding the store flock, polling so a stopped holder
// cannot hang its peers.
func (s *Store) withLock(fn func() error) error {
	deadline := time.Now().Add(s.LockTimeout)
	for {
		l, err := slots.TryLock(filepath.Join(s.dir, ".lock"))
		if err != nil {
			return fmt.Errorf("stance lock: %w", err)
		}
		if l != nil {
			defer l.Release()
			return fn()
		}
		if time.Now().After(deadline) {
			return errors.New("stance: timed out waiting for the store lock")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

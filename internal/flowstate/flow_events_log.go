package flowstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/LysCongThanh/heavyq/internal/flow"
)

const (
	// eventsRotateAt is the size at which the log moves to events.jsonl.1 (one
	// older generation is kept, so a reader never loses the recent past).
	eventsRotateAt = 512 << 10
	// eventsReadMax bounds what is read per file (above eventsRotateAt, so a whole
	// generation is always read): the log is append-only, so
	// readers look at its tail.
	eventsReadMax = 1 << 20
)

func eventsPath(common string) string { return filepath.Join(Dir(common), "events.jsonl") }

// AppendEvent records one hook observation. Every event has a lane except the
// fleet-level quota_cleared. It is one O_APPEND write, so
// concurrent hooks never interleave inside a line. The caller has already
// redacted e.Reason.
func AppendEvent(common string, e flow.Event) error {
	if e.Kind == "" || (e.Lane == "" && e.Kind != flow.EventQuotaCleared) {
		return errors.New("flow event needs a lane and a kind (a quota_cleared one, a kind)")
	}
	e.At = e.At.UTC()
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(Dir(common), 0o700); err != nil {
		return fmt.Errorf("flow events: %w", err)
	}
	p := eventsPath(common)
	if fi, err := os.Stat(p); err == nil && fi.Size() > eventsRotateAt {
		_ = os.Rename(p, p+".1") // best effort: losing the race only rotates twice
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("flow events: %w", err)
	}
	_, werr := f.Write(append(b, '\n'))
	if err := errors.Join(werr, f.Close()); err != nil {
		return fmt.Errorf("flow events: %w", err)
	}
	return nil
}

// ReadEvents returns the events at or after since, oldest first. Lines that
// are not events (a torn write, a future format) are skipped; a missing log is
// empty.
func ReadEvents(common string, since time.Time) ([]flow.Event, error) {
	var out []flow.Event
	for _, p := range []string{eventsPath(common) + ".1", eventsPath(common)} {
		evs, err := readTail(p)
		if err != nil {
			return nil, err
		}
		for _, e := range evs {
			if !e.At.Before(since) {
				out = append(out, e)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}

// readTail decodes the events in the last eventsReadMax bytes of path.
func readTail(path string) ([]flow.Event, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("flow events: %w", err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("flow events: %w", err)
	}
	cut := fi.Size() > eventsReadMax
	if cut {
		if _, err := f.Seek(fi.Size()-eventsReadMax, io.SeekStart); err != nil {
			return nil, fmt.Errorf("flow events: %w", err)
		}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("flow events: %w", err)
	}
	lines := bytes.Split(data, []byte{'\n'})
	if cut {
		lines = lines[1:] // starts mid-line
	}
	var out []flow.Event
	for _, ln := range lines {
		var e flow.Event
		if len(ln) == 0 || json.Unmarshal(ln, &e) != nil || e.Kind == "" || (e.Lane == "" && e.Kind != flow.EventQuotaCleared) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

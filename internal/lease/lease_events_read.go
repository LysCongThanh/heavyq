package lease

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// Events returns the events of the log, oldest first, reading at most the
// last maxBytes of the file (the log is append-only and never rotated, so
// readers look at its tail). A line cut by the window, and any line that is
// not an event, is skipped. A missing log is an empty one.
func (s *Store) Events(maxBytes int64) ([]Event, error) {
	f, err := os.Open(s.eventsPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	cut := false
	if maxBytes > 0 && fi.Size() > maxBytes {
		if _, err := f.Seek(fi.Size()-maxBytes, io.SeekStart); err != nil {
			return nil, fmt.Errorf("read events: %w", err)
		}
		cut = true
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	lines := bytes.Split(data, []byte{'\n'})
	if cut {
		lines = lines[1:] // the first line starts before the window
	}
	var out []Event
	for _, ln := range lines {
		var e Event
		if len(ln) == 0 || json.Unmarshal(ln, &e) != nil || e.Op == "" || e.Item == "" {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

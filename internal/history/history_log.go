// Package history appends one JSON line per finished run and reads them back.
package history

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Record is one finished (or interrupted) run.
type Record struct {
	End      time.Time `json:"end"`
	Machine  string    `json:"machine"`
	Lane     string    `json:"lane"`
	Prio     int       `json:"prio"`
	Light    bool      `json:"light,omitempty"`
	WaitSecs float64   `json:"wait_s"`
	RunSecs  float64   `json:"run_s"`
	Exit     int       `json:"exit"`
	Cmd      string    `json:"cmd"`
}

// DefaultPath is $HEAVYQ_HISTORY, else $XDG_STATE_HOME/heavyq/history.jsonl,
// else ~/.local/state/heavyq/history.jsonl.
func DefaultPath() string {
	if p := os.Getenv("HEAVYQ_HISTORY"); p != "" {
		return p
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "heavyq", "history.jsonl")
}

// Append writes r to path. A single small O_APPEND write is atomic enough for
// concurrent writers on local filesystems.
func Append(path string, r Record) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ReadLast returns up to n most recent records (oldest first). Corrupt lines
// are skipped.
func ReadLast(path string, n int) ([]Record, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var r Record
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			out = append(out, r)
			if n > 0 && len(out) > n {
				out = out[1:]
			}
		}
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("read %s: %w", path, err)
	}
	return out, nil
}

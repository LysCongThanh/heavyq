package history

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

const (
	// legacyFile is where heavyq wrote before rotation; it stays readable.
	legacyFile  = "history.jsonl"
	monthLayout = "2006-01"
)

// monthlyRe matches the rotated files: history-YYYY-MM.jsonl.
var monthlyRe = regexp.MustCompile(`^history-\d{4}-\d{2}\.jsonl$`)

// DefaultPath is where the next record goes: $HEAVYQ_HISTORY exactly as given
// (a single file, never rotated), else history-YYYY-MM.jsonl for the current
// month (UTC) in $XDG_STATE_HOME/heavyq, else ~/.local/state/heavyq.
func DefaultPath() string { return defaultPathAt(time.Now()) }

func defaultPathAt(now time.Time) string {
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
	return filepath.Join(base, "heavyq", "history-"+now.UTC().Format(monthLayout)+".jsonl")
}

// files lists what ReadLast reads for path, oldest first.
//
// A path named history-YYYY-MM.jsonl stands for the whole rotated set in its
// directory: the pre-rotation history.jsonl (if there is one) and then every
// monthly file in month order, so one query spans month boundaries. Any other
// path is exactly that one file, which is how HEAVYQ_HISTORY opts out of
// rotation.
func files(path string) ([]string, error) {
	if !monthlyRe.MatchString(filepath.Base(path)) {
		return []string{path}, nil
	}
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var monthly []string
	for _, e := range entries {
		if !e.IsDir() && monthlyRe.MatchString(e.Name()) {
			monthly = append(monthly, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(monthly) // zero-padded year-month: name order is time order
	out := make([]string, 0, len(monthly)+1)
	if _, err := os.Stat(filepath.Join(dir, legacyFile)); err == nil {
		out = append(out, filepath.Join(dir, legacyFile))
	}
	return append(out, monthly...), nil
}

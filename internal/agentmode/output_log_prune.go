package agentmode

import (
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	// LogKeep and LogMaxAge bound the saved logs: at most LogKeep files, none
	// older than LogMaxAge. With LogMaxBytes each, the worst case is 120 MiB.
	LogKeep   = 30
	LogMaxAge = 7 * 24 * time.Hour
)

// PruneLogs removes the *.log files of dir that are older than maxAge or
// beyond the newest keep. It is best effort and only ever touches log files
// directly inside dir whose name LogName could have made.
func PruneLogs(dir string, keep int, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type logFile struct {
		path string
		mod  time.Time
	}
	var logs []logFile
	for _, e := range entries {
		if e.IsDir() || !logNameRe.MatchString(e.Name()) {
			continue
		}
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() {
			logs = append(logs, logFile{filepath.Join(dir, e.Name()), info.ModTime()})
		}
	}
	sort.Slice(logs, func(i, j int) bool { return logs[i].mod.After(logs[j].mod) })
	cutoff := time.Now().Add(-maxAge)
	for i, l := range logs {
		if i >= keep || l.mod.Before(cutoff) {
			_ = os.Remove(l.path)
		}
	}
}

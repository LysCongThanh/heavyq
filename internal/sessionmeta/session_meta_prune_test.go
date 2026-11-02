package sessionmeta

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A new session is the moment to forget the records nobody has touched for weeks; an unsafe id never becomes a file name.
func TestNewSessionPrunesOldRecordsAndUnsafeIdsAreNotWritten(t *testing.T) {
	common := t.TempDir()
	_, _, _ = RecordIn(common, Meta{Session: "sess-1", Model: "m"}, now)
	stale := filepath.Join(Dir(common), "long-gone.meta.json")
	if err := os.WriteFile(stale, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-30 * 24 * time.Hour)
	_ = os.Chtimes(stale, old, old)
	_, _, _ = RecordIn(common, Meta{Session: "sess-2", Model: "m"}, now.Add(time.Hour))
	if _, err := os.Stat(stale); err == nil {
		t.Error("an old record must be pruned when a new session starts")
	}
	if _, _, err := RecordIn(common, Meta{Session: "../../x", Model: "m"}, now); err == nil {
		t.Error("an unsafe session id is refused")
	}
	entries, _ := os.ReadDir(Dir(common))
	if len(entries) != 2 {
		t.Errorf("want the two sessions' files only: %v", entries)
	}
}

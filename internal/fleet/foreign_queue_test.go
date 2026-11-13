package fleet

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// writeHistory writes a TSV history in the documented layout: end_epoch machine lane prio light wait_s run_s exit.
func writeHistory(t *testing.T, rows [][]any) string {
	t.Helper()
	var b strings.Builder
	for _, r := range rows {
		parts := make([]string, len(r))
		for i, v := range r {
			parts[i] = fmt.Sprint(v)
		}
		b.WriteString(strings.Join(parts, "\t") + "\n")
	}
	p := filepath.Join(t.TempDir(), "history.tsv")
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadHistoryKeepsTheWindowOnly(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	old, recent := now.Add(-30*time.Hour).Unix(), now.Add(-time.Hour).Unix()
	var rows [][]any
	for i := 0; i < 500; i++ { // older than the window: must not be returned
		rows = append(rows, []any{old + int64(i), "box", "lane-old", 1, 0, 10, 20, 0})
	}
	rows = append(rows,
		[]any{recent, "box", "api", 3, 0, 600, 900, 0},
		[]any{"garbage line"},
		[]any{recent + 1, "build-1", "web", 1, 1, 30, 60, 255},
		[]any{recent + 2, "box", "web", 1, 0, 30, 60, ""}, // no exit recorded: skipped
		[]any{recent + 3, "box", "api", 2, 0, 90, 120, 1},
	)
	p := writeHistory(t, rows)
	got, err := ReadHistoryTSV(HistorySource{Path: p, Columns: config.ForeignColumns, Since: now.Add(-24 * time.Hour), MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Runs) != 3 || got.Skipped != 2 || got.Truncated {
		t.Fatalf("runs=%d skipped=%d truncated=%v: %+v", len(got.Runs), got.Skipped, got.Truncated, got.Runs)
	}
	r := got.Runs[0] // oldest first
	if r.Machine != "box" || r.Lane != "api" || r.Prio != 3 || r.WaitS != 600 || r.RunS != 900 || r.Exit != 0 || !r.End.Equal(time.Unix(recent, 0)) {
		t.Fatalf("first run: %+v", r)
	}
	if got.Runs[1].Machine != "build-1" || !got.Runs[1].Light || got.Runs[1].Exit != 255 {
		t.Fatalf("second run: %+v", got.Runs[1])
	}
	// A big file is read from its end and only as far as the byte bound: the report says it is partial.
	small, err := ReadHistoryTSV(HistorySource{Path: p, Columns: config.ForeignColumns, Since: now.Add(-48 * time.Hour), MaxBytes: 4096})
	if err != nil || !small.Truncated || len(small.Runs) == 0 || len(small.Runs) >= 500 {
		t.Fatalf("bounded read: runs=%d truncated=%v err=%v", len(small.Runs), small.Truncated, err)
	}
	if missing, err := ReadHistoryTSV(HistorySource{Path: filepath.Join(t.TempDir(), "none.tsv"), Columns: config.ForeignColumns}); !errors.Is(err, fs.ErrNotExist) || len(missing.Runs) != 0 {
		t.Fatalf("a missing history is an error, not a history without runs: %+v %v", missing, err)
	}
	if got.Parsed != 4 { // the 3 runs and the older line that ended the read
		t.Fatalf("parsed=%d, want 4", got.Parsed)
	}
}

func TestReadHistoryFollowsTheColumnList(t *testing.T) {
	p := writeHistory(t, [][]any{{"box", 1, 1_800_000_000, "api"}})
	got, err := ReadHistoryTSV(HistorySource{Path: p, Columns: []string{"machine", "exit", "end_epoch", "lane"}})
	if err != nil || len(got.Runs) != 1 || got.Runs[0].Machine != "box" || got.Runs[0].Exit != 1 || got.Runs[0].Lane != "api" {
		t.Fatalf("%+v %v", got, err)
	}
}

// Tickets are files named <prio>-<20-digit ns>-<pid>-<lane>, with ".run" while running. The submitting
// process is real here: a ticket whose process is gone is stale, not waiting.
func TestReadTicketsOfAForeignQueue(t *testing.T) {
	dir := t.TempDir()
	alive := exec.Command("sleep", "30")
	if err := alive.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = alive.Process.Kill(); _ = alive.Wait() })
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	since := time.Unix(1_800_000_000, 123)
	ns := fmt.Sprintf("%020d", since.UnixNano())
	pid, gone := alive.Process.Pid, dead.Process.Pid
	files := map[string]string{
		fmt.Sprintf("1-%s-%d-api.run", ns, pid):  fmt.Sprintf("pid=%d lane=api prio=1 machine=build-1 slot=s2 cmd=bash -c make test TOKEN=abc123", pid),
		fmt.Sprintf("3-%s-%d-web", ns, pid):      fmt.Sprintf("pid=%d lane=web prio=3 cmd=npm test", pid),
		fmt.Sprintf("2-%s-%d-old", ns, gone):     "pid=1 lane=old",
		"history.tsv":                            "",
		"not-a-ticket":                           "",
		fmt.Sprintf("1-%s-%d-bad;lane", ns, pid): "",
		fmt.Sprintf("1-%s-%d-api", ns, pid):      "mid-claim twin of the .run file",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ReadTickets(dir, nil) // nil: the real process table decides
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tickets) != 2 || got.Stale != 1 {
		t.Fatalf("tickets=%+v stale=%d", got.Tickets, got.Stale)
	}
	run, wait := got.Tickets[0], got.Tickets[1]
	if !run.Running || run.Lane != "api" || run.Machine != "build-1" || run.Slot != "s2" || run.Prio != 1 || run.Pid != pid || !run.Since.Equal(since) {
		t.Fatalf("running ticket: %+v", run)
	}
	if strings.Contains(run.Cmd, "abc123") || !strings.Contains(run.Cmd, "make test") {
		t.Fatalf("commands go through redaction: %q", run.Cmd)
	}
	if wait.Running || wait.Lane != "web" || wait.Prio != 3 {
		t.Fatalf("waiting ticket: %+v", wait)
	}
	if _, err := ReadTickets(filepath.Join(dir, "missing"), nil); err == nil {
		t.Fatal("a missing tickets dir is an error the report shows")
	}
}

// Lines are appended as runs end on several machines, so one slightly older than the window can follow newer
// ones: it is dropped but must not end the read, and lines with an impossible time or negative wait are skipped.
func TestReadHistoryToleratesOutOfOrderLines(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	since := now.Add(-24 * time.Hour)
	p := writeHistory(t, [][]any{
		{since.Add(time.Hour).Unix(), "box", "a", 1, 0, 1, 1, 0},
		{since.Add(-30 * time.Minute).Unix(), "box", "late", 1, 0, 1, 1, 0}, // out of order, outside the window
		{since.Add(2 * time.Hour).Unix(), "box", "b", 1, 0, 1, 1, 0},
		{5, "box", "epoch", 1, 0, 1, 1, 0},
		{since.Add(3 * time.Hour).Unix(), "box", "neg", 1, 0, -1, 1, 0},
	})
	got, err := ReadHistoryTSV(HistorySource{Path: p, Columns: config.ForeignColumns, Since: since})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Runs) != 2 || got.Runs[0].Lane != "a" || got.Runs[1].Lane != "b" || got.Skipped != 2 {
		t.Fatalf("runs=%+v skipped=%d", got.Runs, got.Skipped)
	}
}

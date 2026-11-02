package app

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/snapshot"
	"github.com/LysCongThanh/heavyq/internal/tui/render"
	"github.com/LysCongThanh/heavyq/internal/tui/term"
)

// fakeScreen is the Screen the loop writes to: it records every write.
type fakeScreen struct {
	mu     sync.Mutex
	out    strings.Builder
	events chan term.Event
	size   term.Size
}

func newScreen(cols, rows int) *fakeScreen {
	return &fakeScreen{events: make(chan term.Event, 4), size: term.Size{Cols: cols, Rows: rows}}
}
func (f *fakeScreen) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.out.Write(p)
}
func (f *fakeScreen) Size() term.Size           { return f.size }
func (f *fakeScreen) Events() <-chan term.Event { return f.events }
func (f *fakeScreen) Suspend()                  {}
func (f *fakeScreen) raw() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.out.String()
}
func (f *fakeScreen) text() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return render.StripSGR(f.out.String())
}

// typed is a terminal's input: the test writes keys, Run reads them.
func typed(t *testing.T) (*io.PipeWriter, io.Reader) {
	r, w := io.Pipe()
	t.Cleanup(func() { _ = w.Close() })
	return w, r
}

// press is the user pressing keys.
func press(t *testing.T, w io.Writer, keys string) {
	t.Helper()
	if _, err := w.Write([]byte(keys)); err != nil {
		t.Fatal(err)
	}
}

func waitText(t *testing.T, s *fakeScreen, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(s.text(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("screen never showed %q:\n%s", want, s.text())
}

func runLoop(t *testing.T, cfg Config, in io.Reader, scr Screen) <-chan error {
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), cfg, in, scr) }()
	return done
}

func waitDone(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return")
	}
}

func TestRunDrawsHandlesKeysAndRemembersTheMode(t *testing.T) {
	s := busy(t)
	s.GeneratedAt = time.Now()
	w, in := typed(t)
	scr := newScreen(100, 30)
	var saved []render.Mode
	var mu sync.Mutex
	cfg := Config{
		Source: func() (snapshot.Snapshot, error) { return s, nil }, Interval: time.Second,
		Mode: render.ModeFull, Theme: render.Theme{Level: render.LevelColor},
		SaveMode: func(m render.Mode) { mu.Lock(); saved = append(saved, m); mu.Unlock() },
	}
	done := runLoop(t, cfg, in, scr)
	waitText(t, scr, "waiting for the first snapshot")
	waitText(t, scr, "NEEDS YOU (3)")
	press(t, w, "m") // focus mode
	waitText(t, scr, "mode focus")
	press(t, w, "?")
	waitText(t, scr, "KEYS")
	press(t, w, "\x1b") // lone Esc closes help once the timeout resolves it
	waitText(t, scr, "mode focus")
	press(t, w, "q")
	waitDone(t, done)
	mu.Lock()
	defer mu.Unlock()
	if len(saved) != 1 || saved[0] != render.ModeFocus {
		t.Fatalf("saved modes = %v, want [focus]", saved)
	}
	if out := scr.raw(); !strings.Contains(out, term.BSU) || !strings.Contains(out, term.ESU) {
		t.Error("frames are not wrapped in synchronized-update markers")
	}
}

func TestRunActsWithControlThroughTheMailbox(t *testing.T) {
	s := busy(t)
	s.GeneratedAt = time.Now()
	rec := &recorder{}
	w, in := typed(t)
	scr := newScreen(100, 30)
	done := runLoop(t, Config{Source: func() (snapshot.Snapshot, error) { return s, nil }, Interval: time.Second,
		Control: rec, Theme: render.Theme{Level: render.LevelNone}}, in, scr)
	waitText(t, scr, "NEEDS YOU (3)")
	press(t, w, "\tjc")
	waitText(t, scr, "cancel docs")
	press(t, w, "y")
	waitText(t, scr, "cancel requested for docs")
	press(t, w, "q")
	waitDone(t, done)
	if got := rec.got(); len(got) != 1 || !strings.HasPrefix(got[0], "cancel 5-") {
		t.Fatalf("calls %v", got)
	}
}

func TestRunShowsStaleAndErrorWhenBuildsFail(t *testing.T) {
	s := busy(t)
	s.GeneratedAt = time.Now()
	var n int
	var mu sync.Mutex
	src := func() (snapshot.Snapshot, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		if n == 1 {
			return s, nil
		}
		return snapshot.Snapshot{}, io.ErrUnexpectedEOF
	}
	w, in := typed(t)
	scr := newScreen(100, 30)
	done := runLoop(t, Config{Source: src, Interval: 250 * time.Millisecond, Theme: render.Theme{Level: render.LevelNone}}, in, scr)
	waitText(t, scr, "NEEDS YOU (3)")
	waitText(t, scr, "SNAPSHOT FAILED")
	press(t, w, "q")
	waitDone(t, done)
}

func TestRunEndsOnQuitSignalResizeRepaintsAndInputCloseQuits(t *testing.T) {
	s := busy(t)
	s.GeneratedAt = time.Now()
	src := func() (snapshot.Snapshot, error) { return s, nil }
	cfg := Config{Source: src, Interval: time.Second, Theme: render.Theme{Level: render.LevelNone}}

	_, in := typed(t)
	scr := newScreen(100, 30)
	done := runLoop(t, cfg, in, scr)
	waitText(t, scr, "NEEDS YOU (3)")
	scr.events <- term.Event{Kind: term.EvResize, Size: term.Size{Cols: 60, Rows: 20}}
	deadline := time.Now().Add(3 * time.Second)
	for strings.Count(scr.raw(), "\x1b[2J") < 2 { // the first paint and the one after the resize
		if time.Now().After(deadline) {
			t.Fatal("a resize must repaint everything")
		}
		time.Sleep(10 * time.Millisecond)
	}
	scr.events <- term.Event{Kind: term.EvQuit, Sig: "terminated"}
	waitDone(t, done)

	w2, in2 := typed(t)
	scr2 := newScreen(100, 30)
	done2 := runLoop(t, cfg, in2, scr2)
	waitText(t, scr2, "NEEDS YOU (3)")
	_ = w2.Close() // the terminal went away
	waitDone(t, done2)
}

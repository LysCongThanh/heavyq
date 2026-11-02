package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/snapshot"
	"github.com/LysCongThanh/heavyq/internal/tui/input"
	"github.com/LysCongThanh/heavyq/internal/tui/render"
	"github.com/LysCongThanh/heavyq/internal/tui/term"
)

var t0 = time.Date(2026, 11, 2, 12, 4, 31, 0, time.UTC)

// busy loads the render package's busy fixture: the same snapshot the golden frames use.
func busy(t *testing.T) snapshot.Snapshot {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "render", "testdata", "busy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s snapshot.Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// recorder is an Actions that records what was asked.
type recorder struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (r *recorder) add(s string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, s)
	return r.err
}
func (r *recorder) Cancel(id string) error { return r.add("cancel " + id) }
func (r *recorder) Prio(id string, p int) error {
	return r.add("prio " + id + " " + string(rune('0'+p)))
}
func (r *recorder) SetPaused(m string, p bool) error {
	if p {
		return r.add("pause " + m)
	}
	return r.add("resume " + m)
}
func (r *recorder) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func keys(m *Model, s string) Effect {
	var p input.Parser
	var last Effect
	ks := p.Feed([]byte(s))
	ks = append(ks, p.Flush()...) // a trailing lone Escape is the Esc key
	for _, k := range ks {
		last = m.Key(k, t0)
	}
	return last
}

func newBusy(t *testing.T, act Actions) *Model {
	t.Helper()
	m := NewModel(render.ModeFull, 2*time.Second, act, func(lane string, _ snapshot.Snapshot, _ time.Time) []string {
		return []string{"lane " + lane + ": explained"}
	})
	m.Snapshot(busy(t), nil, t0)
	return m
}

func TestNavigationAndSections(t *testing.T) {
	m := newBusy(t, nil)
	if r, _ := m.selected(); r.Kind != render.RowException {
		t.Fatalf("starts on %v, want the first exception", r.Kind)
	}
	keys(m, "j")
	keys(m, "\x1b[B")
	if r, _ := m.selected(); r.Kind != render.RowException || r.Lane != "docs" {
		t.Fatalf("after two downs: %+v", r)
	}
	keys(m, "\t")
	if r, _ := m.selected(); r.Kind != render.RowTicket {
		t.Fatalf("Tab should land on the first ticket, got %v", r.Kind)
	}
	keys(m, "\t")
	if r, _ := m.selected(); r.Kind != render.RowMachine {
		t.Fatalf("second Tab should land on a machine, got %v", r.Kind)
	}
	keys(m, "\t")
	if r, _ := m.selected(); r.Kind != render.RowException {
		t.Fatalf("Tab wraps to the exceptions, got %v", r.Kind)
	}
	keys(m, "G")
	if r, _ := m.selected(); r.Machine != "mac" {
		t.Fatalf("G: %+v", r)
	}
	keys(m, "jjj") // clamps at the end
	if r, _ := m.selected(); r.Machine != "mac" {
		t.Fatalf("down past the end: %+v", r)
	}
	keys(m, "g")
	if r, _ := m.selected(); r.Kind != render.RowException {
		t.Fatalf("g: %+v", r)
	}
}

func TestSelectionSurvivesARefreshAndFallsBackWhenTheRowIsGone(t *testing.T) {
	m := newBusy(t, nil)
	keys(m, "G")
	m.Snapshot(busy(t), nil, t0.Add(2*time.Second))
	if r, _ := m.selected(); r.Machine != "mac" {
		t.Fatalf("selection lost on refresh: %+v", r)
	}
	s := busy(t)
	s.Machines = s.Machines[:1] // mac and linuxi vanish
	m.Snapshot(s, nil, t0.Add(4*time.Second))
	if r, ok := m.selected(); !ok || r.Key == "m:mac" {
		t.Fatalf("selection stayed on a vanished row: %+v", r)
	}
}

func TestFilterTypingKeepsAndClears(t *testing.T) {
	m := newBusy(t, nil)
	keys(m, "/")
	if !m.view.Typing {
		t.Fatal("/ did not start typing")
	}
	keys(m, "we")
	keys(m, "x\x7f") // typed and erased
	if m.view.Filter != "we" {
		t.Fatalf("filter = %q", m.view.Filter)
	}
	keys(m, "\r")
	if m.view.Typing || m.view.Filter != "we" {
		t.Fatalf("Enter must keep the filter: typing=%v filter=%q", m.view.Typing, m.view.Filter)
	}
	for _, r := range m.rows() {
		if r.Kind == render.RowTicket && r.Lane != "web" {
			t.Errorf("row %v passes the filter 'we'", r.Lane)
		}
	}
	keys(m, "\x1b") // Esc in normal mode clears the filter
	if m.view.Filter != "" {
		t.Fatalf("Esc left filter %q", m.view.Filter)
	}
	keys(m, "/ab\x1b")
	if m.view.Typing || m.view.Filter != "" {
		t.Fatalf("Esc while typing must drop it: %+v", m.view)
	}
	keys(m, "/")
	keys(m, "q") // while typing q is a letter, not quit
	if m.view.Filter != "q" {
		t.Fatalf("filter = %q", m.view.Filter)
	}
}

func TestWhyHelpAndModeKeys(t *testing.T) {
	m := newBusy(t, nil)
	keys(m, "\t") // first ticket: api
	keys(m, "\r")
	if m.view.Overlay != render.OverlayWhy || m.view.WhyTitle != "api" || !strings.Contains(m.view.WhyLines[0], "explained") {
		t.Fatalf("why overlay: %+v", m.view)
	}
	keys(m, "\x1b")
	if m.view.Overlay != render.OverlayNone {
		t.Fatal("Esc did not close the overlay")
	}
	keys(m, "g\r") // an exception: its sentence and action
	if m.view.Overlay != render.OverlayWhy || !strings.Contains(strings.Join(m.view.WhyLines, "\n"), "to act: ") {
		t.Fatalf("exception why: %+v", m.view.WhyLines)
	}
	keys(m, "\x1b?")
	if m.view.Overlay != render.OverlayHelp {
		t.Fatal("? did not open help")
	}
	if eff := keys(m, "q"); !eff.Quit {
		t.Fatal("q inside an overlay must still quit")
	}
	m.view.Overlay = render.OverlayNone

	seen := []render.Mode{}
	for i := 0; i < 5; i++ {
		keys(m, "m")
		seen = append(seen, m.Mode)
		if _, ok := m.selected(); !ok && m.Mode != render.ModeLine {
			t.Fatalf("mode %v: nothing selected", m.Mode)
		}
	}
	if seen[0] != render.ModeFocus || seen[4] != render.ModeFull {
		t.Fatalf("m cycles %v", seen)
	}
}

func TestReadOnlyRefusesActions(t *testing.T) {
	m := newBusy(t, nil)
	keys(m, "\t")
	for _, k := range []string{"c", "+", "-", "p"} {
		keys(m, k)
		if v := m.View(t0); !strings.Contains(v.Flash, "--control") || v.Prompt != "" {
			t.Errorf("key %q read-only: flash %q prompt %q", k, v.Flash, v.Prompt)
		}
	}
}

func TestCancelAsksFirst(t *testing.T) {
	rec := &recorder{}
	m := newBusy(t, rec)
	keys(m, "\tj") // docs, running
	if r, _ := m.selected(); r.Lane != "docs" {
		t.Fatalf("selected %+v", r)
	}
	keys(m, "c")
	if v := m.View(t0); !strings.Contains(v.Prompt, "cancel docs") {
		t.Fatalf("prompt = %q", v.Prompt)
	}
	if len(rec.got()) != 0 {
		t.Fatal("cancel went out before the answer")
	}
	keys(m, "n")
	if len(rec.got()) != 0 || m.View(t0).Prompt != "" {
		t.Fatalf("n must drop the prompt without acting: %v", rec.got())
	}
	keys(m, "c")
	eff := keys(m, "y")
	got := rec.got()
	if len(got) != 1 || !strings.HasPrefix(got[0], "cancel 5-") || !eff.Refresh {
		t.Fatalf("calls %v, effect %+v", got, eff)
	}
	keys(m, "c")
	keys(m, "x") // any other key means no
	if len(rec.got()) != 1 {
		t.Fatal("a stray key confirmed the cancel")
	}
}

func TestCancelFailureIsShownNotHidden(t *testing.T) {
	rec := &recorder{err: errors.New("disk full")}
	m := newBusy(t, rec)
	keys(m, "\tcy")
	if v := m.View(t0); !strings.Contains(v.Flash, "disk full") {
		t.Fatalf("flash = %q", v.Flash)
	}
}

func TestPriorityKeys(t *testing.T) {
	rec := &recorder{}
	m := newBusy(t, rec)
	keys(m, "\tjjj") // web: waiting, p4
	if r, _ := m.selected(); r.Lane != "web" || !r.Waiting {
		t.Fatalf("selected %+v", r)
	}
	keys(m, "+")
	keys(m, "-")
	got := rec.got()
	if len(got) != 2 || !strings.HasSuffix(got[0], " 3") || !strings.HasSuffix(got[1], " 5") {
		t.Fatalf("calls %v, want p3 then p5", got)
	}
	keys(m, "kk") // docs: running, cannot change priority
	keys(m, "+")
	if len(rec.got()) != 2 || !strings.Contains(m.View(t0).Flash, "waiting") {
		t.Fatalf("a running job's priority changed: %v / %q", rec.got(), m.View(t0).Flash)
	}
	keys(m, "g") // the red-gate exception has no priority action
	keys(m, "+")
	if len(rec.got()) != 2 {
		t.Fatalf("+ on a run-action exception acted: %v", rec.got())
	}
	keys(m, "j") // the starved exception: raise to the value it names
	keys(m, "+")
	if got := rec.got(); len(got) != 3 || !strings.HasSuffix(got[2], " 3") {
		t.Fatalf("raise exception: %v", got)
	}
}

func TestPauseTogglesTheSelectedMachine(t *testing.T) {
	rec := &recorder{}
	m := newBusy(t, rec)
	keys(m, "G") // mac
	keys(m, "p")
	if got := rec.got(); len(got) != 1 || got[0] != "pause mac" {
		t.Fatalf("calls %v", got)
	}
	s := busy(t)
	s.Machines[2].Paused = true
	m.Snapshot(s, nil, t0)
	keys(m, "p")
	if got := rec.got(); len(got) != 2 || got[1] != "resume mac" {
		t.Fatalf("calls %v", got)
	}
}

func TestStaleAndErrorStates(t *testing.T) {
	m := newBusy(t, nil)
	if v := m.View(t0.Add(time.Second)); v.Err != "" || v.Fetched != t0 {
		t.Fatalf("fresh view: %+v", v)
	}
	m.Snapshot(snapshot.Snapshot{}, errors.New("queue unreadable"), t0.Add(3*time.Second))
	v := m.View(t0.Add(3 * time.Second))
	if v.Err == "" || m.Snap() == nil || v.Fetched != t0 {
		t.Fatalf("after an error the last good snapshot stays and Fetched does not move: %+v", v)
	}
	m.Snapshot(busy(t), nil, t0.Add(5*time.Second))
	if v := m.View(t0.Add(5 * time.Second)); v.Err != "" {
		t.Fatalf("error not cleared by a good snapshot: %+v", v)
	}
}

func TestBellOnlyForNewExceptions(t *testing.T) {
	m := NewModel(render.ModeFull, time.Second, nil, nil)
	s := busy(t)
	if eff := m.Snapshot(s, nil, t0); eff.Bell {
		t.Fatal("the first snapshot must not ring")
	}
	if eff := m.Snapshot(s, nil, t0); eff.Bell {
		t.Fatal("the same exceptions must not ring again")
	}
	s.Exceptions = append(s.Exceptions, snapshot.Exception{ID: "new", Kind: "loop", Severity: "critical", Text: "x"})
	if eff := m.Snapshot(s, nil, t0); !eff.Bell {
		t.Fatal("a new exception must ring")
	}
}

func TestCopySelected(t *testing.T) {
	m := newBusy(t, nil)
	eff := keys(m, "y")
	if eff.Copy != "cd /work/api && heavyq gate run ready" {
		t.Fatalf("copy = %q", eff.Copy)
	}
	keys(m, "\tj")
	if eff := keys(m, "y"); eff.Copy != "npm run build" {
		t.Fatalf("copy = %q", eff.Copy)
	}
	if got := osc52("hi"); got != "\x1b]52;c;aGk=\a" {
		t.Fatalf("osc52 = %q", got)
	}
}

func TestPainterWritesOnlyChanges(t *testing.T) {
	var p painter
	first := string(p.paint([]string{"one", "two", "three"}, false))
	for _, want := range []string{term.BSU, term.ESU, "one", "two", "three"} {
		if !strings.Contains(first, want) {
			t.Errorf("first paint lacks %q", want)
		}
	}
	if got := p.paint([]string{"one", "two", "three"}, false); got != nil {
		t.Fatalf("an identical frame wrote %q", got)
	}
	second := string(p.paint([]string{"one", "TWO", "three"}, false))
	if strings.Contains(second, "one") || strings.Contains(second, "three") || !strings.Contains(second, "TWO") {
		t.Errorf("only line 2 should be rewritten: %q", second)
	}
	shorter := string(p.paint([]string{"one", "TWO"}, false))
	if !strings.Contains(shorter, "\x1b[3;1H\x1b[J") {
		t.Errorf("a shorter frame must clear the rest: %q", shorter)
	}
	full := string(p.paint([]string{"one", "TWO"}, true))
	if !strings.Contains(full, "\x1b[2J") || !strings.Contains(full, "one") {
		t.Errorf("full repaint: %q", full)
	}
}

func TestFetcherNeverOverlapsAndRefreshSkipsTheWait(t *testing.T) {
	var running, maxRunning, calls atomic.Int32
	src := func() (snapshot.Snapshot, error) {
		n := running.Add(1)
		if n > maxRunning.Load() {
			maxRunning.Store(n)
		}
		time.Sleep(30 * time.Millisecond)
		running.Add(-1)
		calls.Add(1)
		return snapshot.Snapshot{}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	refresh := make(chan struct{}, 1)
	out := startFetcher(ctx, src, 10*time.Second, refresh)
	<-out // the first build
	refresh <- struct{}{}
	select {
	case <-out:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh did not trigger a build before the interval")
	}
	cancel()
	if maxRunning.Load() != 1 {
		t.Fatalf("builds overlapped: %d at once", maxRunning.Load())
	}
}

func TestFetcherTurnsAPanicIntoAnError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := startFetcher(ctx, func() (snapshot.Snapshot, error) { panic("bug") }, time.Second, nil)
	r := <-out
	if r.err == nil || !strings.Contains(r.err.Error(), "panicked") {
		t.Fatalf("err = %v", r.err)
	}
}

func TestModeStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "heavyq", "top-mode")
	if _, ok := LoadMode(path); ok {
		t.Fatal("a missing file is not a mode")
	}
	if err := SaveMode(path, render.ModeLanes); err != nil {
		t.Fatal(err)
	}
	if m, ok := LoadMode(path); !ok || m != render.ModeLanes {
		t.Fatalf("LoadMode = %v, %v", m, ok)
	}
	if err := os.WriteFile(path, []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadMode(path); ok {
		t.Fatal("garbage must be ignored")
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("state file perms: %v %v", fi, err)
	}
	if _, ok := LoadMode(""); ok || SaveMode("", render.ModeFull) != nil {
		t.Fatal("an empty path disables the store")
	}
	t.Setenv("XDG_STATE_HOME", "/xdg")
	if got := StatePath(); got != "/xdg/heavyq/top-mode" {
		t.Fatalf("StatePath = %q", got)
	}
}

package statusline

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history/historytest"
	"github.com/LysCongThanh/heavyq/internal/notify"
	"github.com/LysCongThanh/heavyq/internal/sessionmeta"
	"github.com/LysCongThanh/heavyq/internal/tui/render"
)

type countChannel struct{ n atomic.Int32 }

func (*countChannel) Name() string { return "count" }
func (c *countChannel) Send(notify.Message) error {
	c.n.Add(1)
	return nil
}

func stdinFor(root string) string {
	return `{"cwd":"` + root + `","workspace":{"current_dir":"` + root + `"},"session_id":"sess-1",` +
		`"model":{"id":"claude-x","display_name":"X"},"version":"2.1.0",` +
		`"rate_limits":{"five_hour":{"used_percentage":61.2,"resets_at":1},"seven_day":{"used_percentage":20}}}`
}

func opts(stdin string, out io.Writer, ch notify.Channel) Options {
	env := map[string]string{"NO_COLOR": "1", "LANG": "en_US.UTF-8"}
	return Options{Stdin: strings.NewReader(stdin), Stdout: out, Stderr: io.Discard,
		Getenv: func(k string) string { return env[k] }, Now: func() time.Time { return now },
		Channels: notify.Fixed(ch)}
}

func TestRunPrintsSegmentAndLeavesSideEffects(t *testing.T) {
	root, common := repoWith(t, scenarios[2].state) // needs-you
	ch := &countChannel{}
	var out bytes.Buffer
	if code := Run(opts(stdinFor(root), &out, ch)); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if want := "◆ 5/8 proven  ⚑ 1 needs you  ▲ CI wait  ✓ on track · ~14:45\n"; render.StripSGR(out.String()) != want {
		t.Errorf("stdout %q, want %q", out.String(), want)
	}
	// Session meta (internal/sessionmeta): model, version and rate limits, joined later by session id.
	m, err := sessionmeta.Read(common, "sess-1")
	if err != nil || m == nil {
		t.Fatalf("session meta: %v %v", m, err)
	}
	if m.Model != "claude-x" || m.Version != "2.1.0" || m.FiveHourPct == nil || *m.FiveHourPct != 61 || *m.SevenDayPct != 20 {
		t.Errorf("session meta: %+v", m)
	}
	// Notified once, however many times the status line runs.
	for i := 0; i < 4; i++ {
		Run(opts(stdinFor(root), io.Discard, ch))
	}
	if ch.n.Load() != 1 {
		t.Errorf("notified %d times, want 1", ch.n.Load())
	}
}

func TestRunSaysNothingWithoutStateOrRepo(t *testing.T) {
	root, common := repoWith(t, "")
	var out bytes.Buffer
	ch := &countChannel{}
	if code := Run(opts(stdinFor(root), &out, ch)); code != 0 || out.Len() != 0 {
		t.Errorf("no state: exit %d, output %q", code, out.String())
	}
	if _, err := os.Stat(filepath.Join(common, "heavyq")); err == nil {
		t.Error("a repository without heavyq state must not be written to")
	}
	for name, in := range map[string]string{"outside a repo": stdinFor(t.TempDir()), "garbage": "{nope", "empty": "", "wrong types": `{"cwd":7,"workspace":[]}`} {
		out.Reset()
		if code := Run(opts(in, &out, ch)); code != 0 || out.Len() != 0 {
			t.Errorf("%s: exit %d, output %q", name, code, out.String())
		}
	}
}

func TestRunFallsBackToTheWorkingDirectory(t *testing.T) {
	root, _ := repoWith(t, scenarios[0].state)
	o := opts("", &bytes.Buffer{}, &countChannel{})
	var out bytes.Buffer
	o.Stdout, o.Cwd = &out, func() string { return root }
	Run(o)
	if !strings.HasPrefix(out.String(), "◆ 5/8") {
		t.Errorf("got %q", out.String())
	}
}

func TestRunNeverHangsOnAStdinThatNeverCloses(t *testing.T) {
	root, _ := repoWith(t, scenarios[0].state)
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()
	var out bytes.Buffer
	o := opts("", &out, &countChannel{})
	o.Stdin, o.Cwd = pr, func() string { return root }
	start := time.Now()
	Run(o)
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v on a stdin that never closes", d)
	}
	if !strings.HasPrefix(out.String(), "◆ 5/8") {
		t.Errorf("got %q", out.String())
	}
}

func TestStaleStateSpawnsOneRefreshPerWindow(t *testing.T) {
	stale := fx{proven: 5, n: 9, updated: "2026-11-02T13:50:00Z"}.json() // 10 min old
	root, _ := repoWith(t, stale)
	var spawned []string
	o := func() Options {
		x := opts(stdinFor(root), io.Discard, &countChannel{})
		x.SpawnRefresh = func(dir string) { spawned = append(spawned, dir) }
		return x
	}
	for i := 0; i < 5; i++ {
		Run(o())
	}
	if len(spawned) != 1 || spawned[0] != root {
		t.Fatalf("spawned %v, want exactly one refresh in the repository", spawned)
	}
	// A fresh state spawns nothing.
	root2, _ := repoWith(t, scenarios[0].state)
	spawned = nil
	x := opts(stdinFor(root2), io.Discard, &countChannel{})
	x.SpawnRefresh = func(string) { t.Error("a fresh state must not be refreshed") }
	Run(x)
}

func TestWrapAppendsToTheLastLineAndPassesStdinThrough(t *testing.T) {
	root, _ := repoWith(t, scenarios[0].state)
	var out bytes.Buffer
	o := opts(stdinFor(root), &out, &countChannel{})
	// The user's command reads the same JSON; here it prints one field and a second line.
	o.Wrap = `jq_free=$(cat); case "$jq_free" in *claude-x*) echo "line one"; echo "model ok";; *) echo "NO STDIN"; esac`
	if code := Run(o); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if want := "line one\nmodel ok │ ◆ 5/8 ✓ ~15:30\n"; out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestWrapSurvivesAFailingOrSilentCommandAndAnEmptySegment(t *testing.T) {
	root, _ := repoWith(t, scenarios[0].state)
	var out bytes.Buffer
	o := opts(stdinFor(root), &out, &countChannel{})
	o.Wrap = "exit 3"
	if code := Run(o); code != 3 || out.String() != "◆ 5/8 ✓ ~15:30\n" {
		t.Errorf("silent failing command: exit %d, out %q", code, out.String())
	}
	// No state: the user's line passes through untouched.
	bare, _ := repoWith(t, "")
	out.Reset()
	o = opts(stdinFor(bare), &out, &countChannel{})
	o.Wrap = "echo mine"
	if code := Run(o); code != 0 || out.String() != "mine\n" {
		t.Errorf("empty segment: exit %d, out %q", code, out.String())
	}
}

func TestParseInputIsTolerant(t *testing.T) {
	in := ParseInput([]byte(`{"cwd":"/a","workspace":{"current_dir":"/b"},"model":{"id":"m"},"version":3,"session_id":"s",
"rate_limits":{"five_hour":{"used_percentage":150},"seven_day":"x"}}`))
	if in.Dir() != "/b" || in.Model != "m" || in.Version != "" || in.SessionID != "s" || in.FiveHour != 100 || in.SevenDay != -1 {
		t.Errorf("%+v", in)
	}
	if in := ParseInput([]byte(`{"cwd":"/a"}`)); in.Dir() != "/a" || in.FiveHour != -1 {
		t.Errorf("%+v", in)
	}
}

// The status line has 5 ms (Claude Code debounces at 300 ms and a slow script
// blocks updates). Best of many: noise on a shared machine only slows a run
// down. It exercises the whole path on a real repository: parse, find the
// repository without git, read the state, render, session record, notify check.
func TestSegmentPathUnder5ms(t *testing.T) {
	if testing.Short() || historytest.RaceEnabled {
		t.Skip("timing test: needs a full run without -race")
	}
	root, _ := repoWith(t, scenarios[2].state)
	ch := &countChannel{}
	in := stdinFor(root)
	best := time.Hour
	for i := 0; i < 40; i++ {
		var out bytes.Buffer
		start := time.Now()
		Run(opts(in, &out, ch))
		if d := time.Since(start); d < best {
			best = d
		}
		if out.Len() == 0 {
			t.Fatal("no segment")
		}
	}
	t.Logf("best of 40: %v", best)
	if best > 5*time.Millisecond {
		t.Errorf("status line path took %v, budget 5ms", best)
	}
}

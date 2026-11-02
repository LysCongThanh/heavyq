package statusline

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/LysCongThanh/heavyq/internal/flowview"
	"github.com/LysCongThanh/heavyq/internal/notify"
	"github.com/LysCongThanh/heavyq/internal/tui/render"
)

// Options of Run. The zero value of every function field means "the real one".
type Options struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
	GOOS   string
	Now    func() time.Time
	ASCII  bool   // --ascii
	Wrap   string // --wrap: the user's own status-line command; the segment is appended to its last line
	Cwd    func() string
	// SpawnRefresh starts a detached `heavyq flow refresh` in dir. nil when
	// that command does not exist (it ships with the flow engine): nothing is
	// spawned then.
	SpawnRefresh func(dir string)
	NoNotify     bool
	// Channels overrides the notification channels (tests); nil picks them from the environment.
	Channels func() []notify.Channel
}

// Run is `heavyq statusline --segment` (and --wrap). It never fails the status
// line: any problem leaves the segment out, and the exit code is 0 unless a
// wrapped command failed. Output comes first and the side effects (session
// record, refresh, notification) after it, all of them cheap or detached.
func Run(o Options) int {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Getenv == nil {
		o.Getenv = os.Getenv
	}
	raw := ReadInput(o.Stdin)
	in := ParseInput(raw)
	if in.CWD == "" && o.Cwd != nil {
		in.CWD = o.Cwd()
	}
	th := render.ThemeFromEnv(o.Getenv, o.ASCII)
	now := o.Now()

	var segment string
	var after func()
	if common, ok := flowview.CommonDirOf(in.Dir()); ok {
		st, err := flowview.Read(flowview.Path(common))
		if err != nil {
			st = nil // no state, a bad one or another schema: say nothing
		}
		if line := Segment(st, now, th); len(line) > 0 {
			segment = RenderLine(line, th.Level)
		}
		if st != nil { // heavyq is in use here; elsewhere nothing is written
			after = func() { o.sideEffects(common, in, st, now) }
		}
	}

	out, code := segment, 0
	if o.Wrap != "" {
		var existing string
		existing, code = runWrapped(o.Wrap, raw, in.Dir(), o.Stderr)
		out = Join(existing, segment, th)
	}
	if out != "" {
		_, _ = fmt.Fprintln(o.Stdout, out)
	}
	if after != nil {
		after()
	}
	return code
}

// sideEffects are the cheap or detached things a status-line call also does.
func (o Options) sideEffects(common string, in Input, st *flowview.State, now time.Time) {
	recordSessionMeta(common, in, now)
	if o.SpawnRefresh != nil && NeedsRefresh(st, now) && ClaimRefresh(common, now) {
		o.SpawnRefresh(in.Dir())
	}
	if !o.NoNotify {
		notify.Check(common, st.NotifyFacts(now), notify.Options{
			Budget:   notify.BudgetFromEnv(o.Getenv),
			Channels: o.channels(),
		})
	}
}

func (o Options) channels() func() []notify.Channel {
	if o.Channels != nil {
		return o.Channels
	}
	return func() []notify.Channel { return notify.DefaultChannels(o.Getenv, o.goos()) }
}

func (o Options) goos() string {
	if o.GOOS != "" {
		return o.GOOS
	}
	_, goos := notify.SystemEnv()
	return goos
}

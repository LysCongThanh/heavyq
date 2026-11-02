package app

import (
	"context"
	"encoding/base64"
	"io"
	"time"

	"github.com/LysCongThanh/heavyq/internal/snapshot"
	"github.com/LysCongThanh/heavyq/internal/tui/input"
	"github.com/LysCongThanh/heavyq/internal/tui/render"
	"github.com/LysCongThanh/heavyq/internal/tui/term"
)

// Screen is the terminal as the loop sees it (term.Session implements it).
type Screen interface {
	Write(p []byte) (int, error)
	Size() term.Size
	Events() <-chan term.Event
	Suspend()
}

// Config wires the loop.
type Config struct {
	Source   Source // builds a snapshot
	Why      func(lane string, s snapshot.Snapshot, now time.Time) []string
	Control  Actions       // nil: read-only
	Interval time.Duration // snapshot period
	Mode     render.Mode
	SaveMode func(render.Mode) // remembers the mode when m changes it; may be nil
	Theme    render.Theme
	Bell     bool             // ring the terminal bell when a new exception appears
	Now      func() time.Time // default time.Now
}

const (
	frameEvery = 100 * time.Millisecond // ~10 fps: live counters tick, unchanged frames cost nothing
	escWait    = 50 * time.Millisecond  // how long a lone Escape waits for the rest of a sequence
	maxCopy    = 4096                   // OSC 52 payload limit, in bytes of text
)

// Run draws and handles input until the user quits, ctx ends, or input
// closes. The caller owns the terminal (term.Start / Restore).
func Run(ctx context.Context, cfg Config, in io.Reader, scr Screen) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	model := NewModel(cfg.Mode, cfg.Interval, cfg.Control, cfg.Why)
	refresh := make(chan struct{}, 1)
	results := startFetcher(ctx, cfg.Source, cfg.Interval, refresh)
	chunks := readChunks(in)
	var parser input.Parser
	var pt painter
	size, full := scr.Size(), true
	tick := time.NewTicker(frameEvery)
	defer tick.Stop()
	var escTimer <-chan time.Time

	draw := func() {
		lines := render.Frame(model.Snap(), model.Mode, model.View(now()),
			render.Options{Cols: size.Cols, Rows: size.Rows, Theme: cfg.Theme})
		if out := pt.paint(lines, full); out != nil {
			_, _ = scr.Write(out)
		}
		full = false
	}
	apply := func(e Effect) (quit bool) {
		switch {
		case e.Quit:
			return true
		case e.Suspend:
			scr.Suspend()
		}
		if e.Refresh {
			select {
			case refresh <- struct{}{}:
			default:
			}
		}
		if e.Redraw {
			full = true
		}
		if e.Copy != "" {
			_, _ = scr.Write([]byte(osc52(e.Copy)))
		}
		if e.Bell && cfg.Bell {
			_, _ = scr.Write([]byte("\a"))
		}
		return false
	}
	handle := func(keys []input.Key) (quit bool) {
		for _, k := range keys {
			before := model.Mode
			eff := model.Key(k, now())
			if model.Mode != before && cfg.SaveMode != nil {
				cfg.SaveMode(model.Mode)
			}
			if apply(eff) {
				return true
			}
		}
		return false
	}

	draw()
	for {
		select {
		case <-ctx.Done():
			return nil
		case b, ok := <-chunks:
			if !ok {
				return nil // input closed: nobody can answer
			}
			if handle(parser.Feed(b)) {
				return nil
			}
			escTimer = nil
			if parser.Pending() {
				escTimer = time.After(escWait)
			}
		case <-escTimer:
			escTimer = nil
			if handle(parser.Flush()) {
				return nil
			}
		case r, ok := <-results:
			if ok && apply(model.Snapshot(r.snap, r.err, now())) {
				return nil
			}
		case ev := <-scr.Events():
			switch ev.Kind {
			case term.EvQuit:
				return nil
			case term.EvResize, term.EvResume:
				size, full = ev.Size, true
			}
		case <-tick.C:
		}
		draw()
	}
}

// readChunks reads input on its own goroutine. The goroutine ends when the
// reader fails or closes; a read blocked on a terminal ends with the process.
func readChunks(in io.Reader) <-chan []byte {
	out := make(chan []byte, 8)
	go func() {
		defer close(out)
		buf := make([]byte, 256)
		for {
			n, err := in.Read(buf)
			if n > 0 {
				out <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				return
			}
		}
	}()
	return out
}

// osc52 is the escape sequence that puts text on the system clipboard
// (write-only; terminals that do not allow it ignore it).
func osc52(text string) string {
	if len(text) > maxCopy {
		text = text[:maxCopy]
	}
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\a"
}

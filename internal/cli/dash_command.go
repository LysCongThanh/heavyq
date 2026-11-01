package cli

import (
	"flag"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/dash"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/queue"
	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// dashCommand serves the dashboard until interrupted.
func dashCommand(cfg *config.Config, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("dash", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", "127.0.0.1:0", "listen address (loopback only; port 0 picks a free one)")
	ctl := fs.Bool("control", false, "allow cancel, priority changes and pause/resume from the page")
	interval := fs.Duration("interval", 2*time.Second, "snapshot period of the live stream")
	open := fs.Bool("open", false, "open the dashboard in the default browser")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *interval < time.Second {
		fmt.Fprintln(stderr, "heavyq: --interval must be at least 1s")
		return ExitUsage
	}
	q, err := queue.Open(QueueDir())
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	probes := newProbeCache(cfg.Queue.ProbeTimeout.Duration)
	host, _ := os.Hostname()
	srv, err := dash.New(dash.Options{
		Control: *ctl, Interval: *interval, Version: version(), Host: host, Mailbox: controlBox(),
		Snapshot: func() (snapshot.Snapshot, error) { return buildSnapshot(cfg, probes, cpuSampleInterval) },
		History:  func(n int) ([]history.Record, error) { return history.ReadLast(history.DefaultPath(), n) },
		Ticket: func(id string) (bool, bool) {
			t, ok := q.Find(id)
			return ok, ok && t.Running
		},
		Machine: func(name string) bool { _, ok := cfg.Machine(name); return ok },
	})
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	ln, err := srv.Listen(*addr)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	url := fmt.Sprintf("http://%s/#token=%s", ln.Addr(), srv.Token())
	mode := "read-only (add --control to act from the page)"
	if *ctl {
		mode = "control enabled: the page can cancel jobs, change priorities and pause machines"
	}
	fmt.Fprintf(stdout, "heavyq dashboard: %s\n%s. Ctrl-C to stop.\n", url, mode)
	if *open {
		if err := openBrowser(url); err != nil {
			fmt.Fprintln(stderr, "heavyq: --open:", err)
		}
	}
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 5 * time.Second}
	if err := hs.Serve(ln); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	return 0
}

// openBrowser opens url without putting the token on any command line
// (other users can read /proc/*/cmdline): the browser gets a 0600 file in
// the queue directory that redirects to the dashboard.
func openBrowser(url string) error {
	page := filepath.Join(QueueDir(), "dash-open.html")
	html := fmt.Sprintf(`<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=%s"><a href="%s">heavyq dashboard</a>`,
		template.HTMLEscapeString(url), template.HTMLEscapeString(url))
	if err := os.WriteFile(page, []byte(html), 0o600); err != nil {
		return err
	}
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	cmd := exec.Command(name, page)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

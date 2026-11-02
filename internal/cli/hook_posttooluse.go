package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/deadline"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/hook"
	"github.com/LysCongThanh/heavyq/internal/ledgerindex"
	"github.com/LysCongThanh/heavyq/internal/mentor"
	"github.com/LysCongThanh/heavyq/internal/queue"
)

// mentorOffEnv turns the mentor off for a process and its children: the
// escape every note names, and a switch for one session.
const mentorOffEnv = "HEAVYQ_MENTOR"

// claudePostToolUse is `heavyq hook claude-posttooluse`. Claude Code calls it
// after every Bash tool call (as PostToolUse, or PostToolUseFailure when the
// command exited non-zero). It fails open like the PreToolUse hook: whatever
// goes wrong, or when the 15 ms budget is spent, it prints nothing and exits
// 0, and the problem goes to hook.log. It reads the lane's records from the
// ledger index, never the ledger.
func claudePostToolUse(cfgPath string, explicit bool, stdin io.Reader, stdout io.Writer) (rc int) {
	defer func() {
		if p := recover(); p != nil {
			hookLog("error: mentor: panic: %v", p)
			rc = 0
		}
	}()
	if os.Getenv(mentorOffEnv) == "off" {
		return 0
	}
	in, err := io.ReadAll(io.LimitReader(stdin, maxHookInput))
	if err != nil {
		hookLog("error: mentor: read stdin: %v", err)
		return 0
	}
	out, err := deadline.RunWithin(mentorBudget(), func(context.Context) ([]byte, error) {
		return postToolUseNote(cfgPath, explicit, in)
	})
	if err != nil {
		hookLog("error: mentor: %v", err)
		return 0
	}
	if len(out) == 0 {
		return 0
	}
	if _, err := fmt.Fprintf(stdout, "%s\n", out); err != nil {
		hookLog("error: mentor: write stdout: %v", err)
	}
	return 0
}

// mentorBudget is the hook's time budget. The test-only switch HEAVYQ_TEST=1
// (see internal/metrics) widens it: the e2e suite runs a -race binary on
// machines of any load, and a missed deadline would only make a test flaky.
func mentorBudget() time.Duration {
	if os.Getenv("HEAVYQ_TEST") == "1" {
		return 10 * time.Second
	}
	return deadline.HookBudget
}

// postToolUseNote runs the mentor for one payload and returns the hook JSON to
// print, nil for "say nothing" (not our call, no event, or a held-out event).
func postToolUseNote(cfgPath string, explicit bool, payload []byte) ([]byte, error) {
	call, _, err := hook.ParsePostToolUse(payload)
	if err != nil || call == nil {
		return nil, err
	}
	cfg, err := config.Load(cfgPath, explicit)
	if err != nil {
		return nil, err
	}
	store, ledger, err := mentorStore()
	if err != nil {
		return nil, err
	}
	mc := mentorConfig(cfg)
	lane, recs, err := mentorLane(ledger, call, time.Now(), mc.Window)
	if err != nil || lane == "" {
		return nil, err
	}
	d, err := store.Evaluate(mentor.Input{Lane: lane, Session: call.Session, Records: recs, Now: time.Now()}, mc)
	if err != nil || d == nil {
		return nil, err
	}
	hookLog("mentor: %s level %d arm %s lane %s event %s", d.Entry.Rule, d.Entry.Level, d.Entry.Arm, lane, d.Entry.Event)
	if d.Message == "" {
		return nil, nil // held out: logged, silent
	}
	return hook.PostContext(call.Event, d.Message)
}

// mentorLane finds the lane this tool call ran on and its records. The
// command's own --lane wins, then $HEAVYQ_LANE, then what the PreToolUse hook
// would have derived (the work tree's name), then the cwd's name. The first
// candidate whose newest record ended within window is the lane; failing
// that, the first with any record (so pending outcomes still resolve).
func mentorLane(ledger string, call *hook.PostCall, now time.Time, window time.Duration) (string, []history.Record, error) {
	// Candidates are computed lazily: the work tree's name costs a git call, so
	// it is only asked for when the cheaper ones found no fresh record.
	sources := []func() string{
		func() string { return call.Lane },
		func() string { return os.Getenv("HEAVYQ_LANE") },
		func() string { return worktreeLane(call.Cwd) },
		func() string {
			if call.Cwd == "" {
				return ""
			}
			return filepath.Base(call.Cwd)
		},
	}
	tried := map[string]bool{}
	var firstLane string
	var firstRecs []history.Record
	for _, src := range sources {
		l := src()
		if l == "" {
			continue
		}
		if l = queue.SanitizeLane(l); tried[l] {
			continue
		}
		tried[l] = true
		recs, _, err := ledgerindex.LaneRuns(ledger, l)
		if err != nil {
			return "", nil, err
		}
		if len(recs) == 0 {
			continue
		}
		if now.Sub(recs[len(recs)-1].End) <= window {
			return l, recs, nil
		}
		if firstLane == "" {
			firstLane, firstRecs = l, recs
		}
	}
	return firstLane, firstRecs, nil
}

// mentorStore returns the mentor's files: durable ones beside the ledger,
// per-lane state in the queue directory. It also returns the ledger path.
func mentorStore() (*mentor.Store, string, error) {
	ledger := history.DefaultPath()
	if ledger == "" {
		return nil, "", errors.New("no state directory for the ledger")
	}
	return &mentor.Store{Dir: filepath.Join(filepath.Dir(ledger), "mentor"),
		LaneDir: filepath.Join(QueueDir(), "mentor")}, ledger, nil
}

// mentorConfig is the mentor's tuning from the [mentor] section.
func mentorConfig(cfg *config.Config) mentor.Config {
	c := mentor.DefaultConfig()
	c.HoldoutPct = cfg.MentorHoldoutPct()
	c.AutoMute = mentor.AutoMute{Enabled: cfg.MentorAutoMute(), MinEvents: cfg.MentorMinEvents(), MinLiftPP: cfg.MentorMinLiftPP()}
	return c
}

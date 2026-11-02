package cli

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/flow"
	"github.com/LysCongThanh/heavyq/internal/flowstate"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/hook"
)

// flowLane is the lane of the hook's work tree (itemLane), and, when the lane
// was named after the work tree (no $HEAVYQ_LANE), the work tree's path: a lane
// whose work tree is later removed drops out of the flow state.
func flowLane(cwd string) (lane, worktree string) {
	if cwd == "" {
		return "", ""
	}
	if l := os.Getenv("HEAVYQ_LANE"); l != "" {
		return laneOf(l), ""
	}
	if top, err := gitutil.TopLevel(cwd); err == nil {
		return laneOf(filepath.Base(top)), top
	}
	return laneOf(filepath.Base(cwd)), ""
}

// quotaNotificationKind classifies a Notification about the usage limit
// (quota_auto_resume_*): one that says agents are resuming lifts the quota, any
// other says they are held until it returns.
func quotaNotificationKind(t string) (string, bool) {
	rest, ok := strings.CutPrefix(t, "quota_auto_resume")
	if !ok {
		return "", false
	}
	for _, w := range []string{"started", "resumed", "resuming", "done", "complete", "completed", "succeeded"} {
		if strings.HasSuffix(rest, "_"+w) {
			return flow.EventQuotaCleared, true
		}
	}
	return flow.EventQuota, true
}

// noteQuota keeps what a usage-limit event means beyond its log line: a stop
// leaves the fleet-level mark that any session's next activity takes; a
// resume notice lifts it at once.
func noteQuota(ev flow.Event, common string) {
	if ev.Kind == flow.EventQuotaCleared {
		flowstate.TakeQuotaMark(ev.At, 0)
		return
	}
	if err := flowstate.MarkQuota(common, ev.At); err != nil {
		hookLog("claude-flow: quota mark: %v", err)
	}
}

// noteFlowActivity records that session did something: its own block, if it
// left one, is cleared, and so is the fleet's usage limit once the stops are
// past the grace, in every repository the stops were recorded in. It returns
// the shared directories (of the repositories) it wrote events to: their
// states must be refreshed at once.
func noteFlowActivity(session string, now time.Time) (wrote []string, err error) {
	if common, lane, ok := flowstate.TakeBlockMark(session); ok {
		if e := flowstate.AppendEvent(common, flow.Event{At: now, Kind: flow.EventCleared, Lane: lane, Session: session}); e != nil {
			return nil, e
		}
		wrote = append(wrote, common)
	}
	for _, common := range flowstate.TakeQuotaMark(now, quotaGrace()) {
		if e := flowstate.AppendEvent(common, flow.Event{At: now, Kind: flow.EventQuotaCleared, Session: session}); e != nil {
			err = e
			continue
		}
		wrote = append(wrote, common)
	}
	return wrote, err
}

// refreshFlowOf refreshes (forced) the state of the repository whose shared
// directory is common, from its main work tree; one whose main work tree is
// gone is left for its next event.
func refreshFlowOf(who, common string) {
	if filepath.Base(common) != ".git" {
		return
	}
	dir := filepath.Dir(common)
	if _, err := os.Stat(dir); err == nil {
		autoRefreshFlow(who, defaultConfigLoader, dir, true)
	}
}

// quotaGraceEnv overrides flow.QuotaGrace for the hooks (a duration such as 0s
// or 30s); the test suite sets it so that it need not wait two minutes.
const quotaGraceEnv = "HEAVYQ_FLOW_QUOTA_GRACE"

func quotaGrace() time.Duration {
	if d, err := time.ParseDuration(os.Getenv(quotaGraceEnv)); err == nil && d >= 0 {
		return d
	}
	return flow.QuotaGrace
}

// flowActivityOn are the hook events, beside PostToolUse, that show an agent
// is alive: a session (re)started, a person answered, a turn ended.
var flowActivityOn = map[string]bool{"SessionStart": true, "UserPromptSubmit": true, "Stop": true}

// noteFlowActivityOfHook is noteFlowActivity for those events; it returns the
// shared directories whose state must be refreshed at once.
func noteFlowActivityOfHook(in hook.Input) []string {
	if flowDisabled() || !flowActivityOn[in.Event] {
		return nil
	}
	wrote, err := noteFlowActivity(in.SessionID, time.Now())
	if err != nil {
		hookLog("%s: flow activity: %v", in.Event, err)
	}
	return wrote
}

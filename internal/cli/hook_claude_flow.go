package cli

import (
	"bytes"
	"io"
	"time"

	"github.com/LysCongThanh/heavyq/internal/flow"
	"github.com/LysCongThanh/heavyq/internal/flowstate"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/hook"
	"github.com/LysCongThanh/heavyq/internal/sessionmeta"
)

// flowHookBudget bounds the hook handler itself (resolve the repository, append
// an event). The refresh that follows has its own budget (flowAutoBudget).
const flowHookBudget = 3 * time.Second

// claudeFlow is `heavyq hook claude-flow`: the observer behind `--features flow`.
// It handles PermissionRequest, Notification, StopFailure, PostModelSwitch and
// PostToolUse, and it only observes: it records what happened in the flow
// events log and never prints a decision, so a permission is still the
// person's to give or deny. It fails open like every hook (see runHookHandler).
func claudeFlow(stdin io.Reader, stdout, stderr io.Writer) int {
	// The runner parses the common fields; this hook also needs the event-specific
	// ones, so it keeps the bytes and hands the runner a copy.
	raw, err := io.ReadAll(io.LimitReader(stdin, maxHookInput))
	if err != nil {
		hookLog("claude-flow: error: read stdin: %v", err)
		return 0
	}
	return runHookHandler("claude-flow", flowHookBudget, bytes.NewReader(raw), stdout, stderr, func(in hook.Input) (hookReply, error) {
		return handleFlow(in, raw)
	})
}

func handleFlow(in hook.Input, raw []byte) (hookReply, error) {
	if flowDisabled() || in.Cwd == "" {
		return hookReply{}, nil
	}
	p := parseFlowPayload(raw)
	now := time.Now()
	ev := flow.Event{At: now, Session: in.SessionID}
	force := true // a person is needed, or an answer arrived: show it within this event
	switch in.Event {
	case "PostToolUse":
		return flowCleared(in, now)
	case "PermissionRequest":
		ev.Kind, ev.Class = flow.EventBlocked, flow.ClassPermissionRequest
		ev.Reason = flow.ClassPermissionRequest + ": " + p.toolSummary()
	case "Notification":
		if kind, ok := quotaNotificationKind(p.NotificationType); ok {
			ev.Kind, ev.Detail = kind, oneLine(p.NotificationType, 80)
			break
		}
		class, ok := notificationClass(p.NotificationType)
		if !ok {
			return hookReply{}, nil
		}
		ev.Kind, ev.Class, ev.Reason = flow.EventBlocked, class, flowReason(class, p.Message)
	case "StopFailure":
		ev.Kind, ev.Class, ev.Detail = flow.EventStopFailure, flow.ClassStopFailure, p.errorWord()
		ev.Reason = flow.ClassStopFailure + ": " + ev.Detail
	case "PostModelSwitch":
		if p.ToModel == "" {
			return hookReply{}, nil
		}
		ev.Kind, ev.Detail = flow.EventModelSwitch, oneLine(p.ToModel, 80)
		force = false // a change point, not an alarm
	default:
		return hookReply{}, nil
	}
	common, err := gitutil.CommonDir(in.Cwd)
	if err != nil {
		return hookReply{}, nil // not a repository: nothing to keep state in
	}
	ev.Lane, ev.Worktree = flowLane(in.Cwd)
	if ev.Lane == "" {
		return hookReply{}, nil
	}
	if err := flowstate.AppendEvent(common, ev); err != nil {
		return hookReply{}, err
	}
	switch ev.Kind {
	case flow.EventBlocked, flow.EventStopFailure:
		if err := flowstate.MarkBlocked(in.SessionID, common, ev.Lane); err != nil {
			hookLog("claude-flow: block mark: %v", err)
		}
		if ev.Kind == flow.EventStopFailure && ev.Detail == "rate_limit" {
			noteQuota(ev, common)
		}
	case flow.EventQuota, flow.EventQuotaCleared:
		noteQuota(ev, common)
	case flow.EventModelSwitch:
		if err := sessionmeta.RecordModelSwitch(common, in.SessionID, p.FromModel, p.ToModel, now); err != nil {
			hookLog("claude-flow: model switch not kept: %v", err)
		}
	}
	hookLog("claude-flow %s lane %s session %s: %s", in.Event, ev.Lane, in.SessionID, firstNonEmpty(ev.Reason, ev.Detail))
	return hookReply{After: func() { autoRefreshFlow("claude-flow", defaultConfigLoader, in.Cwd, force) }}, nil
}

// flowCleared handles PostToolUse: the agent is working again, so its lane is
// not waiting any more, and a usage limit that stopped agents, if there was one,
// is lifted. In the common case (nothing blocked, no quota) this is two failed
// reads and no git call.
func flowCleared(in hook.Input, now time.Time) (hookReply, error) {
	wrote, err := noteFlowActivity(in.SessionID, now)
	if err != nil || len(wrote) == 0 {
		return hookReply{}, err
	}
	hookLog("claude-flow PostToolUse session %s: cleared", in.SessionID)
	return hookReply{After: func() {
		for _, common := range wrote {
			refreshFlowOf("claude-flow", common)
		}
		autoRefreshFlow("claude-flow", defaultConfigLoader, in.Cwd, true)
	}}, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// claudeSessionMeta is `heavyq hook claude-sessionmeta`: it reads the Claude
// Code status-line JSON on stdin (model.id, version, session_id,
// rate_limits.five_hour/seven_day) and stores the session meta record the
// ledger joins agent and model on. The status line agent (or a user's own
// status-line script) pipes its input here; internal/sessionmeta exports the
// same function for in-process callers. It prints nothing (it is not the
// status line's text), always exits 0, and says why in hook.log.
func claudeSessionMeta(stdin io.Reader) (rc int) {
	defer func() {
		if p := recover(); p != nil {
			hookLog("claude-sessionmeta: error: panic: %v", p)
			rc = 0
		}
	}()
	raw, err := io.ReadAll(io.LimitReader(stdin, maxHookInput))
	if err != nil {
		hookLog("claude-sessionmeta: error: read stdin: %v", err)
		return 0
	}
	if _, _, err := sessionmeta.RecordStatusline(raw, "", time.Now()); err != nil {
		hookLog("claude-sessionmeta: error: %v", err)
	}
	return 0
}

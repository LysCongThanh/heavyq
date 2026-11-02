package cli

import (
	"io"
	"os"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/hook"
	"github.com/LysCongThanh/heavyq/internal/steer"
)

// claudeStop is `heavyq hook claude-stop [event]`: the stop gate for Stop,
// SubagentStop, TaskCompleted and TeammateIdle. The optional argument names the
// event when the settings route several through one command; without it the
// hook input's hook_event_name decides, and Stop is the default. It fails open
// (see runHookHandler), and with steering off it costs one `git rev-parse`.
func claudeStop(cfgPath string, explicit bool, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	argEvent := steer.Event("")
	if len(args) > 1 {
		hookLog("claude-stop: error: unexpected arguments %v", args[1:])
		return 0
	}
	if len(args) == 1 {
		ev, ok := steer.ParseEvent(args[0])
		if !ok {
			hookLog("claude-stop: error: unknown event %q", args[0])
			return 0
		}
		argEvent = ev
	}
	return runHookHandler("claude-stop", stopBudget, stdin, stdout, stderr, func(in hook.Input) (hookReply, error) {
		return handleStop(cfgPath, explicit, argEvent, in)
	})
}

func handleStop(cfgPath string, explicit bool, argEvent steer.Event, in hook.Input) (hookReply, error) {
	ev := argEvent
	if ev == "" {
		ev, _ = steer.ParseEvent(in.Event)
	}
	if ev == "" {
		ev = steer.EventStop
	}
	if in.Cwd == "" {
		return hookReply{}, nil
	}
	common, err := gitutil.CommonDir(in.Cwd)
	if err != nil {
		return hookReply{}, nil // not a repository: nothing to steer
	}
	mode, src := steer.Effective(os.Getenv(steer.EnvVar), steer.ReadStored(common))
	if mode == steer.Off {
		if src != "stored" && src != steer.EnvVar {
			hookLog("claude-stop: %s", src)
		}
		return hookReply{}, nil
	}
	if in.SessionID == "" {
		hookLog("claude-stop: no valid session id, so nothing can be counted: allowed")
		return hookReply{}, nil
	}
	enforce := ev == steer.EventStop || mode == steer.Destroy
	claims := (ev == steer.EventStop || ev == steer.EventSubagentStop) && !in.StopHookActive && steer.ClaimsDone(in.LastAssistantMessage)
	if !enforce && !claims {
		return hookReply{}, nil // facts cost git calls: only gather what a decision can use
	}
	cfg, err := config.Load(cfgPath, explicit)
	if err != nil {
		return hookReply{}, err
	}
	facts, err := gatherSteerFacts(cfg, in.Cwd, itemLane(in.Cwd))
	if err != nil {
		return hookReply{}, err
	}
	sessions := steer.OpenSessions(common)
	st, _ := sessions.Load(in.SessionID)
	d := steer.DecideStop(steer.StopInput{Event: ev, Mode: mode, StopHookActive: in.StopHookActive,
		LastMessage: in.LastAssistantMessage, Facts: facts, State: st})
	hookLog("claude-stop %s (%s, %s): %s", ev, mode, in.SessionID, d.Why)
	return stopReply(ev, d, func() {
		if err := sessions.Save(in.SessionID, d.State); err != nil {
			hookLog("claude-stop: warning: session state not saved: %v", err)
		}
	})
}

// stopReply turns a decision into the hook's output. Stop and SubagentStop
// speak JSON on stdout with exit 0; TaskCompleted and TeammateIdle block with
// exit 2 and the reason on stderr (Claude Code's contract for those events).
func stopReply(ev steer.Event, d steer.StopDecision, save func()) (hookReply, error) {
	switch d.Action {
	case steer.Block:
		if ev == steer.EventTaskCompleted || ev == steer.EventTeammateIdle {
			return hookReply{Stderr: d.Text, Exit: 2, After: save}, nil
		}
		out, err := hook.StopBlockOutput(d.Text)
		return hookReply{Stdout: out, After: save}, err
	case steer.Continue:
		out, err := hook.ContextOutput(string(ev), d.Text)
		return hookReply{Stdout: out, After: save}, err
	}
	if d.Note != "" {
		out, err := hook.SystemMessageOutput(d.Note)
		return hookReply{Stdout: out}, err
	}
	return hookReply{}, nil
}

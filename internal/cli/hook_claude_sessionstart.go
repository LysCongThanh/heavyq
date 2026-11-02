package cli

import (
	"io"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/hook"
	"github.com/LysCongThanh/heavyq/internal/steer"
)

// sessionRecordTTL is how long a session's steering record is kept.
const sessionRecordTTL = 14 * 24 * time.Hour

// claudeSessionStart is `heavyq hook claude-sessionstart`: it injects the brief
// as additionalContext on startup, resume and compact, so compaction cannot
// erase the thread. It also records, once per session, what was already red
// (the baseline the stop gate respects) and a delivery canary. It does not
// depend on the steering mode. Fails open, and says nothing when the repository
// has no goal, no items and no red gate.
func claudeSessionStart(cfgPath string, explicit bool, stdin io.Reader, stdout, stderr io.Writer) int {
	return runHookHandler("claude-sessionstart", sessionStartBudget, stdin, stdout, stderr, func(in hook.Input) (hookReply, error) {
		return handleSessionStart(cfgPath, explicit, in)
	})
}

func handleSessionStart(cfgPath string, explicit bool, in hook.Input) (hookReply, error) {
	if in.Cwd == "" {
		return hookReply{}, nil
	}
	common, err := gitutil.CommonDir(in.Cwd)
	if err != nil {
		return hookReply{}, nil // not a repository
	}
	cfg, err := config.Load(cfgPath, explicit)
	if err != nil {
		return hookReply{}, err
	}
	facts, err := gatherSteerFacts(cfg, in.Cwd, itemLane(in.Cwd))
	if err != nil {
		return hookReply{}, err
	}
	now := time.Now()
	sessions := steer.OpenSessions(common)
	var st steer.SessionState
	known := false
	if in.SessionID != "" {
		st, known = sessions.Load(in.SessionID)
	}
	if !known {
		st = steer.SessionState{Started: now}
		// A baseline taken after compaction would hide what this session broke:
		// without a record from the real start, there is none (fail open).
		if in.Source != "compact" {
			st.HasBaseline, st.Baseline = true, facts.AllFailing()
		}
	}
	b := steer.BuildBrief(facts, st)
	// A lane's active stance rides along with the brief, so compaction cannot
	// erase it either (internal/stance, docs/stances.md).
	stanceText := activeStanceText(in.Cwd, itemLane(in.Cwd))
	if b.Empty() && stanceText != "" {
		out, err := hook.ContextOutput("SessionStart", stanceText)
		if err != nil {
			return hookReply{}, err
		}
		hookLog("claude-sessionstart (%s, %s): stance only, %d bytes", in.Source, in.SessionID, len(stanceText))
		return hookReply{Stdout: out, After: saveSession(sessions, in.SessionID, st)}, nil
	}
	if b.Empty() {
		if steer.ReadStored(common) == steer.Off {
			return hookReply{}, nil // a repository that never steers keeps no record
		}
		return hookReply{After: saveSession(sessions, in.SessionID, st)}, nil
	}
	if token, err := steer.NewCanary(); err == nil {
		b.Canary = token
		st.Canary, st.CanaryAt, st.Transcript = token, now, in.TranscriptPath
	}
	text := b.Render()
	if stanceText != "" {
		text += "\n" + stanceText
	}
	out, err := hook.ContextOutput("SessionStart", text)
	if err != nil {
		return hookReply{}, err
	}
	hookLog("claude-sessionstart (%s, %s): brief %d bytes, stance %d bytes", in.Source, in.SessionID, len(b.Render()), len(stanceText))
	return hookReply{Stdout: out, After: func() {
		sessions.Prune(sessionRecordTTL, now)
		saveSession(sessions, in.SessionID, st)()
	}}, nil
}

// saveSession returns a function that stores st for the session, if it has a
// valid id; failures are logged, never surfaced.
func saveSession(s steer.Sessions, id string, st steer.SessionState) func() {
	return func() {
		if id == "" {
			return
		}
		if err := s.Save(id, st); err != nil {
			hookLog("warning: session state not saved: %v", err)
		}
	}
}

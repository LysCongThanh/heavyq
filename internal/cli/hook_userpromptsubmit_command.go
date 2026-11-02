package cli

import (
	"io"
	"time"

	"github.com/LysCongThanh/heavyq/internal/hook"
	"github.com/LysCongThanh/heavyq/internal/stance"
)

// promptHookBudget bounds the UserPromptSubmit handler. It runs once per user
// prompt, not per tool call, so it may read git (the stance dirs, the churn
// log); past it the hook says nothing.
const promptHookBudget = time.Second

// claudeUserPromptSubmit is `heavyq hook claude-userpromptsubmit`: when the
// user's prompt names a stance (a keyword of a stance, like "breakthrough" or
// "đột phá"), or the lane's evidence calls for one, it adds the compact stance
// as additionalContext and records the lane's state. It fails open like every
// hook (runHookHandler): whatever goes wrong, it prints nothing and exits 0.
// HEAVYQ_STANCES=off turns it off.
func claudeUserPromptSubmit(stdin io.Reader, stdout, stderr io.Writer) int {
	return runHookHandler("claude-userpromptsubmit", promptHookBudget, stdin, stdout, stderr, func(in hook.Input) (hookReply, error) {
		return handlePrompt(stance.DefaultEnv(in.Cwd), in)
	})
}

// handlePrompt runs the stance engine for the lane of the hook's cwd and
// replies with the compact stance when the lane enters one because of this
// prompt, and with nothing otherwise, which is the usual answer.
func handlePrompt(env stance.Env, in hook.Input) (hookReply, error) {
	if stance.Disabled() || in.Cwd == "" || in.Prompt == "" || (in.Event != "" && in.Event != "UserPromptSubmit") {
		return hookReply{}, nil
	}
	entry, err := env.OnPrompt(in.Cwd, itemLane(in.Cwd), in.SessionID, in.Prompt)
	if err != nil || entry == nil {
		return hookReply{}, err
	}
	out, err := hook.ContextOutput("UserPromptSubmit", entry.Context)
	if err != nil {
		return hookReply{}, err
	}
	hookLog("claude-userpromptsubmit (%s): entered stance %s (%s)", in.SessionID, entry.Stance, entry.Trigger)
	return hookReply{Stdout: out}, nil
}

// activeStanceText is the compact form of the stance the lane of cwd is in, for
// the SessionStart brief: it is how a stance survives compaction. "" when the
// lane is in none (or stances are off).
func activeStanceText(cwd, lane string) string {
	if stance.Disabled() || cwd == "" {
		return ""
	}
	text, _ := stance.DefaultEnv(cwd).ActiveFor(cwd, lane)
	return text
}

package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/LysCongThanh/heavyq/internal/deadline"
	"github.com/LysCongThanh/heavyq/internal/hook"
	"github.com/LysCongThanh/heavyq/internal/stance"
)

// promptHookBudget bounds the UserPromptSubmit handler. It runs once per user
// prompt, not per tool call, so it may read git (the stance dirs, the churn
// log); past it the hook says nothing.
const promptHookBudget = 250 * time.Millisecond

// claudeUserPromptSubmit is `heavyq hook claude-userpromptsubmit`: when the
// user's prompt names a stance (a keyword of a stance, like "breakthrough" or
// "đột phá"), or the lane's evidence calls for one, it adds the compact stance
// as additionalContext and records the lane's state. It fails open like every
// hook: whatever goes wrong, it prints nothing and exits 0. HEAVYQ_STANCES=off
// turns it off.
func claudeUserPromptSubmit(stdin io.Reader, stdout io.Writer) (rc int) {
	defer func() {
		if p := recover(); p != nil {
			hookLog("error: stance prompt hook: panic: %v", p)
			rc = 0
		}
	}()
	if stance.Disabled() {
		return 0
	}
	in, err := io.ReadAll(io.LimitReader(stdin, maxHookInput))
	if err != nil {
		hookLog("error: read stdin: %v", err)
		return 0
	}
	type decision struct {
		out []byte
		why string
	}
	d, err := deadline.RunWithin(promptHookBudget, func(context.Context) (decision, error) {
		out, why, err := hook.DecidePrompt(in, hook.PromptOptions{Enter: enterStanceForPrompt})
		return decision{out, why}, err
	})
	if err != nil {
		hookLog("error: stance prompt hook: %v", err)
		return 0
	}
	if d.out == nil {
		return 0
	}
	if _, err := fmt.Fprintf(stdout, "%s\n", d.out); err != nil {
		hookLog("error: write stdout: %v", err)
		return 0
	}
	hookLog("%s", d.why)
	return 0
}

// enterStanceForPrompt runs the stance engine for the lane of the hook's cwd
// and returns the text to inject ("" when the lane does not enter a stance).
func enterStanceForPrompt(cwd, session, prompt string) (string, error) {
	if cwd == "" {
		var err error
		if cwd, err = os.Getwd(); err != nil {
			return "", err
		}
	}
	entry, err := stance.DefaultEnv(cwd).OnPrompt(cwd, itemLane(cwd), session, prompt)
	if err != nil || entry == nil {
		return "", err
	}
	return entry.Context, nil
}

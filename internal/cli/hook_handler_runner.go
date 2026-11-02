package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/LysCongThanh/heavyq/internal/deadline"
	"github.com/LysCongThanh/heavyq/internal/hook"
)

// Budgets of the hooks that read repository state (items, leases, proofs, git).
// They are looser than the 15 ms of a per-tool-call hook, because they fire
// once per session or turn, and tighter than Claude Code's own timeout: past the
// budget the hook says nothing and the agent carries on.
const (
	sessionStartBudget = 5 * time.Second
	stopBudget         = 5 * time.Second
)

// hookReply is what a hook handler wants said. The zero value says nothing.
type hookReply struct {
	// Stdout is the JSON object for Claude Code (exit 0).
	Stdout []byte
	// Stderr and Exit: the way TaskCompleted and TeammateIdle block is exit 2
	// with the reason on stderr.
	Stderr string
	Exit   int
	// After runs once the reply is final and within budget: session state is
	// written here, never inside the handler, which may be abandoned at the
	// deadline and must not touch anything the caller could also write.
	After func()
}

// runHookHandler is the shell every heavyq hook except PreToolUse shares. A
// new hook (PostToolUse, UserPromptSubmit) supplies only handle. It reads the
// hook JSON (bounded), runs handle under budget, and prints the reply. It fails
// open: bad input, an error, a panic or a missed deadline prints nothing and
// exits 0, and hook.log says why.
func runHookHandler(name string, budget time.Duration, stdin io.Reader, stdout, stderr io.Writer,
	handle func(in hook.Input) (hookReply, error)) (rc int) {
	defer func() {
		if p := recover(); p != nil {
			hookLog("%s: error: panic: %v", name, p)
			rc = 0
		}
	}()
	raw, err := io.ReadAll(io.LimitReader(stdin, maxHookInput))
	if err != nil {
		hookLog("%s: error: read stdin: %v", name, err)
		return 0
	}
	in, err := hook.ParseInput(raw)
	if err != nil {
		hookLog("%s: error: %v", name, err)
		return 0
	}
	reply, err := deadline.RunWithin(budget, func(context.Context) (hookReply, error) { return handle(in) })
	if err != nil {
		hookLog("%s: error: %v", name, err)
		return 0
	}
	if reply.After != nil {
		reply.After()
	}
	if len(reply.Stdout) > 0 {
		if _, err := fmt.Fprintf(stdout, "%s\n", reply.Stdout); err != nil {
			hookLog("%s: error: write stdout: %v", name, err)
			return 0
		}
	}
	if reply.Stderr != "" {
		fmt.Fprintln(stderr, reply.Stderr)
	}
	// The reply is out: bring the flow state up to date (debounced; SessionStart
	// and Stop only, see flow_refresh.go). Never before the reply, never at PreToolUse.
	refreshFlowAfterHook(name, in)
	return reply.Exit
}

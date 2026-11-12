package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/hook"
	"github.com/LysCongThanh/heavyq/internal/queue"
	"github.com/LysCongThanh/heavyq/internal/redact"
)

const hookUsage = `usage:
  heavyq hook claude-pretooluse                 (called by Claude Code; reads hook JSON on stdin)
  heavyq hook claude-userpromptsubmit           (called by Claude Code on each prompt: enters a stance on a keyword)
  heavyq hook claude-posttooluse                (the mentor: PostToolUse and PostToolUseFailure, see docs/mentor.md)
  heavyq hook claude-sessionstart               (SessionStart: the brief; called by Claude Code)
  heavyq hook claude-stop [Stop|SubagentStop|TaskCompleted|TeammateIdle]   (the stop gate; called by Claude Code)
  heavyq hook claude-flow                       (observe only: permission prompts, idle, StopFailure, model switches; see docs/flow.md)
  heavyq hook claude-sessionmeta                (reads status-line JSON on stdin; keeps the session's model and agent version)
  heavyq hook install claude [--scope local|project|user] [--features queue,brief,stop,stance,flow] [--print] [--force]   (--force: even with the plugin enabled)
  heavyq hook uninstall claude [--scope local|project|user] [--print]
`

// maxHookInput bounds what the hook reads from stdin.
const maxHookInput = 1 << 20

func hookCommand(cfgPath string, explicit bool, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, hookUsage)
		return ExitUsage
	}
	switch args[0] {
	case "claude-pretooluse":
		return claudePreToolUse(cfgPath, explicit, stdin, stdout)
	case "claude-userpromptsubmit":
		return claudeUserPromptSubmit(stdin, stdout, stderr)
	case "claude-posttooluse":
		return claudePostToolUse(cfgPath, explicit, stdin, stdout)
	case "claude-sessionstart":
		return claudeSessionStart(cfgPath, explicit, stdin, stdout, stderr)
	case "claude-stop":
		return claudeStop(cfgPath, explicit, args[1:], stdin, stdout, stderr)
	case "claude-flow":
		return claudeFlow(stdin, stdout, stderr)
	case "claude-sessionmeta":
		return claudeSessionMeta(stdin)
	case "install", "uninstall":
		return hookInstallCommand(cfgPath, explicit, args[0] == "install", args[1:], stdout, stderr)
	}
	fmt.Fprint(stderr, hookUsage)
	return ExitUsage
}

// claudePreToolUse fails open: whatever goes wrong, it prints nothing and
// exits 0, so Claude Code runs the original command. Problems are logged.
func claudePreToolUse(cfgPath string, explicit bool, stdin io.Reader, stdout io.Writer) (rc int) {
	defer func() {
		if p := recover(); p != nil {
			hookLog("error: panic: %v", p)
			rc = 0
		}
	}()
	in, err := io.ReadAll(io.LimitReader(stdin, maxHookInput))
	if err != nil {
		hookLog("error: read stdin: %v", err)
		return 0
	}
	cfg, err := config.Load(cfgPath, explicit)
	if err != nil {
		hookLog("error: %v", err)
		return 0
	}
	if !cfg.HookEnabled() {
		return 0
	}
	bin, err := resolveBinary(cfg)
	if err != nil {
		hookLog("error: %v", err)
		return 0
	}
	rules := append([]config.HookRule{}, cfg.Hook.Rules...)
	if cfg.HookDefaultRules() {
		rules = append(rules, hook.DefaultRules...)
	}
	out, why, err := hook.Decide(in, hook.Options{Rules: rules, Permission: cfg.Hook.Permission,
		MaxWait: cfg.Hook.MaxWait.Duration, Binary: bin, Lane: os.Getenv("HEAVYQ_LANE"),
		Active: os.Getenv("HEAVYQ_ACTIVE") != "", LaneFor: worktreeLane, SessionSink: recordHookSession,
		ToolTimeouts: hook.ToolTimeouts{Default: os.Getenv("BASH_DEFAULT_TIMEOUT_MS"), Max: os.Getenv("BASH_MAX_TIMEOUT_MS")},
		CacheEnv:     cfg.Cache.Env, LookPath: exec.LookPath})
	if err != nil {
		hookLog("error: %v", err)
		return 0
	}
	if out == nil {
		if strings.HasPrefix(why, hook.LeftMatched) {
			hookLog("%s", why) // a rule matched, so the user expects a rewrite: say why there was none
		}
		return 0
	}
	if _, err := fmt.Fprintf(stdout, "%s\n", out); err != nil {
		hookLog("error: write stdout: %v", err)
		return 0
	}
	hookLog("%s", redact.Command(why)) // the rewrite quotes the command line, env assignments included
	return 0
}

// recordHookSession hands the Claude session id to the runs of lane through
// <queue dir>/sessions/<lane>. It is best effort: a failure costs the session
// label in the ledger, never the rewrite.
func recordHookSession(lane, id string) {
	q, err := queue.Open(QueueDir())
	if err == nil {
		err = q.WriteSession(lane, id, time.Now())
	}
	if err != nil {
		hookLog("warning: session not recorded: %v", err)
	}
}

// worktreeLane is the name of the git work tree containing cwd, else cwd's
// own name.
func worktreeLane(cwd string) string {
	if cwd == "" {
		return ""
	}
	if top, err := gitutil.TopLevel(cwd); err == nil {
		return filepath.Base(top)
	}
	return filepath.Base(cwd)
}

// resolveBinary is hook.binary, else "heavyq" when PATH finds this very
// binary, else this binary's absolute path.
func resolveBinary(cfg *config.Config) (string, error) {
	if cfg.Hook.Binary != "" {
		return cfg.Hook.Binary, nil
	}
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate own binary: %w", err)
	}
	if real, err := filepath.EvalSymlinks(self); err == nil {
		self = real
	}
	if p, err := exec.LookPath("heavyq"); err == nil {
		if real, err := filepath.EvalSymlinks(p); err == nil && real == self {
			return "heavyq", nil
		}
	}
	return self, nil
}

// hookLog appends one line to $XDG_STATE_HOME/heavyq/hook.log (rotated at
// 1 MiB). Logging must never break the hook, so errors are ignored.
func hookLog(format string, a ...any) {
	hist := history.DefaultPath()
	if hist == "" {
		return
	}
	path := filepath.Join(filepath.Dir(hist), "hook.log")
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	if fi, err := os.Stat(path); err == nil && fi.Size() > 1<<20 {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, a...))
}

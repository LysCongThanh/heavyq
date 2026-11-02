package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/hook"
	"github.com/LysCongThanh/heavyq/internal/steer"
)

const steerUsage = `usage:
  heavyq steer                                  show the mode in force
  heavyq steer off|guard|destroy [--apply] [--scope local|project|user]

off      no stop gate (the default); the brief still works.
guard    Stop blocks while this lane holds claimed items that are not proven on this tree: at most 3 blocks per
         session, never twice in a row on an unchanged situation, and a tree that was red before the session
         started never blocks.
destroy  no cap, and SubagentStop, TaskCompleted and TeammateIdle are gated too. The exits are proof,
         heavyq item block <id> "needs: ...", heavyq item release <id>, and HEAVYQ_STEER=off.
         Claude Code ends a turn after 8 Stop-hook blocks in a row unless CLAUDE_CODE_STOP_HOOK_BLOCK_CAP is 0:
         --apply writes that to the "env" block of the Claude Code settings (a .bak is kept).

The mode is stored per repository in <git common dir>/heavyq/steer. HEAVYQ_STEER overrides it (off is the
kill switch). See docs/steer.md.
`

// steerCommand implements `heavyq steer [off|guard|destroy] [--apply]`. It
// needs no config file, so it works even when the config is broken.
func steerCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("steer", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, steerUsage) }
	apply := fs.Bool("apply", false, "destroy: set "+steer.NativeCapEnv+"=0 in the Claude Code settings")
	scope := fs.String("scope", "local", "with --apply: local (.claude/settings.local.json), project or user")
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return ExitUsage
		}
		if fs.NArg() == 0 {
			break
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if len(pos) > 1 {
		fs.Usage()
		return ExitUsage
	}
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	common, err := gitutil.CommonDir(wd)
	if err != nil {
		fmt.Fprintf(stderr, "heavyq: %s is not inside a git work tree, so there is no repository to steer\n", wd)
		return ExitUsage
	}
	if len(pos) == 0 {
		if *apply {
			fs.Usage()
			return ExitUsage
		}
		return steerShow(common, stdout)
	}
	mode, err := steer.ParseMode(pos[0])
	if err != nil {
		fmt.Fprintf(stderr, "heavyq: %v\n\n%s", err, steerUsage)
		return ExitUsage
	}
	if *apply && mode != steer.Destroy {
		fmt.Fprintln(stderr, "heavyq: --apply is for destroy: it lifts Claude Code's own cap on Stop-hook blocks")
		return ExitUsage
	}
	if err := steer.WriteStored(common, mode); err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return 1
	}
	fmt.Fprintf(stdout, "steer: %s (stored in %s)\n", mode, steer.ModeFile(common))
	if env := os.Getenv(steer.EnvVar); env != "" {
		fmt.Fprintf(stdout, "note: %s=%s overrides it in this environment\n", steer.EnvVar, env)
	}
	if mode != steer.Destroy {
		return 0
	}
	return steerDestroyCap(*apply, *scope, stdout, stderr)
}

func steerShow(common string, stdout io.Writer) int {
	stored := steer.ReadStored(common)
	mode, src := steer.Effective(os.Getenv(steer.EnvVar), stored)
	if src == "stored" {
		fmt.Fprintf(stdout, "steer: %s (stored in %s)\n", mode, steer.ModeFile(common))
		return 0
	}
	fmt.Fprintf(stdout, "steer: %s (%s overrides the stored mode, which is %s)\n", mode, steer.EnvVar, stored)
	return 0
}

// steerDestroyCap tells how to lift Claude Code's native cap on Stop-hook
// blocks, which destroy mode needs, or does it with --apply: an idempotent
// write of the env line into the chosen settings file, with a .bak.
func steerDestroyCap(apply bool, scope string, stdout, stderr io.Writer) int {
	line := steer.NativeCapEnv + "=0"
	if !apply {
		fmt.Fprintf(stdout, "Claude Code ends a turn after 8 Stop-hook blocks in a row; destroy has no cap of its own, so lift the native one:\n"+
			"  export %s\n"+
			"  or in the settings:  \"env\": {\"%s\": \"0\"}\n"+
			"  or let heavyq write it:  heavyq steer destroy --apply [--scope local|project|user]\n", line, steer.NativeCapEnv)
		return 0
	}
	path, err := settingsFile(scope)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	merged, changed, err := hook.MergeEnv(old, steer.NativeCapEnv, "0")
	if err != nil {
		fmt.Fprintf(stderr, "heavyq: %s: %v (left unchanged)\n", path, err)
		return ExitUsage
	}
	if !changed {
		fmt.Fprintf(stdout, "already set: %s in %s\n", line, path)
		return 0
	}
	if err := hook.WriteSettings(path, merged); err != nil {
		fmt.Fprintf(stderr, "heavyq: write %s: %v\n", path, err)
		return 1
	}
	fmt.Fprintf(stdout, "set %s in %s (restart Claude Code to pick it up)\n", line, path)
	if len(old) > 0 {
		fmt.Fprintf(stdout, "  backup: %s.bak\n", path)
	}
	return 0
}

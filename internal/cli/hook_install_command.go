package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/hook"
)

// hookInstallCommand merges (or removes) the heavyq PreToolUse hook in a
// Claude Code settings file.
func hookInstallCommand(cfgPath string, explicit, install bool, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "claude" {
		fmt.Fprintf(stderr, "heavyq: only `claude` is supported (Claude Code)\n\n%s", hookUsage)
		return ExitUsage
	}
	fs := flag.NewFlagSet("hook", flag.ContinueOnError)
	fs.SetOutput(stderr)
	scope := fs.String("scope", "local", "local (.claude/settings.local.json), project (.claude/settings.json) or user (~/.claude/settings.json)")
	printOnly := fs.Bool("print", false, "print the resulting settings instead of writing them")
	force := fs.Bool("force", false, "install even when the heavyq Claude Code plugin is enabled (the hook would run twice)")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "heavyq: unexpected argument %q\n", fs.Arg(0))
		return ExitUsage
	}
	if install && !*force && !*printOnly {
		if path := pluginEnabledIn(); path != "" {
			fmt.Fprint(stderr, pluginRefusal(path))
			return ExitUsage
		}
	}
	path, err := settingsFile(*scope)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	var merged []byte
	var changed bool
	command := ""
	if install {
		cfg, err := config.Load(cfgPath, explicit)
		if err != nil {
			fmt.Fprintln(stderr, "heavyq:", err)
			return ExitUsage
		}
		bin, err := resolveBinary(cfg)
		if err != nil {
			fmt.Fprintln(stderr, "heavyq:", err)
			return ExitUsage
		}
		command = hook.ShellQuote(bin) + " " + hook.Subcommand
		merged, changed, err = hook.MergeInstall(old, command)
		if err != nil {
			fmt.Fprintf(stderr, "heavyq: %s: %v (left unchanged)\n", path, err)
			return ExitUsage
		}
		if bin != "heavyq" && *scope == "project" {
			fmt.Fprintf(stderr, "heavyq: warning: %s is not on PATH; the shared project settings will contain this absolute path\n", bin)
		}
	} else {
		merged, changed, err = hook.MergeUninstall(old)
		if err != nil {
			fmt.Fprintf(stderr, "heavyq: %s: %v (left unchanged)\n", path, err)
			return ExitUsage
		}
	}
	if *printOnly {
		_, _ = stdout.Write(merged)
		return 0
	}
	switch {
	case !changed && install:
		fmt.Fprintf(stdout, "already installed in %s\n", path)
	case !changed:
		fmt.Fprintf(stdout, "not installed in %s\n", path)
	default:
		if err := hook.WriteSettings(path, merged); err != nil {
			fmt.Fprintf(stderr, "heavyq: write %s: %v\n", path, err)
			return 1
		}
		if install {
			fmt.Fprintf(stdout, "installed PreToolUse hook (matcher Bash): %s\n  in %s\n", command, path)
		} else {
			fmt.Fprintf(stdout, "removed the heavyq hook from %s\n", path)
		}
		if len(old) > 0 {
			fmt.Fprintf(stdout, "  backup: %s.bak\n", path)
		}
	}
	return 0
}

// settingsFile resolves the scope against the current project root (git
// top level, else cwd) or the Claude Code config dir.
func settingsFile(scope string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root := wd
	if top, err := gitutil.TopLevel(wd); err == nil {
		root = top
	}
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil && scope == "user" {
			return "", err
		}
		dir = filepath.Join(home, ".claude")
	}
	return hook.SettingsPath(scope, root, dir)
}

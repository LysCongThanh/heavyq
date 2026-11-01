// Package cli implements the heavyq command line.
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// Version is set at build time via -ldflags.
var Version = "dev"

// Exit codes owned by heavyq (anything else is the command's own code).
const (
	ExitUsage       = 2
	ExitMaxWait     = 75
	ExitRemoteDown  = 76
	ExitRemoteSetup = 77
	ExitCancelled   = 78
	ExitConnLost    = 255
)

const usage = `heavyq - a priority, resource-aware queue for heavy commands

usage:
  heavyq [--config FILE] <prio 1-9> [flags] -- <command...>
  heavyq [--config FILE] status [--json]
  heavyq [--config FILE] dash [--control] [--addr 127.0.0.1:0] [--open]
  heavyq cancel <ticket|pid> | reprio <ticket|pid> <1-9>
  heavyq [--config FILE] pause|resume <machine>
  heavyq history [-n N]
  heavyq [--config FILE] cache ls|stats|clear
  heavyq [--config FILE] hook claude-pretooluse | install claude | uninstall claude
  heavyq version

run flags:
  --lane NAME          label shown in status (default: $HEAVYQ_LANE or cwd name)
  --light              light job: may use light slots and jump ahead into them
  --max-wait DUR       give up after DUR in the queue (exit 75); "90", "90s", "5m"
  --remote WHERE       local | auto | <remote name> (default: auto when safe)
  --needs-ram          place by free RAM only (ignore load)
  --repo DIR           repository to ship for remote runs (default: git root of cwd)
  --ref REF            commit to run remotely (default: HEAD)
  --base-ref R/B       also recreate refs/remotes/R/B remotely (e.g. origin/main)
  --with REPO[@REF]    sibling repo needed next to the main one (repeatable)
  --cache              reuse the result of an identical run on the same clean
                       commit, or wait for one that is running right now
  -v, --verbose        explain cache decisions

Lower priority numbers run first; equal priorities are FIFO.
Exit codes: the command's own; 2 usage; 75 max-wait; 76 remote unreachable
too long; 77 remote setup failed; 78 cancelled by an operator; 255
connection to the remote lost.
`

// Main runs heavyq with args (without the program name) and returns the exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	cfgPath, explicit := config.DefaultPath(), os.Getenv("HEAVYQ_CONFIG") != ""
	if len(args) >= 2 && args[0] == "--config" {
		cfgPath, explicit, args = args[1], true, args[2:]
	}
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return ExitUsage
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	case "version", "--version":
		fmt.Fprintln(stdout, "heavyq", version())
		return 0
	case "history":
		return historyCommand(args[1:], stdout, stderr)
	case "hook":
		return hookCommand(cfgPath, explicit, args[1:], os.Stdin, stdout, stderr)
	}
	cfg, err := config.Load(cfgPath, explicit)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	if args[0] == "status" || args[0] == "--status" {
		return statusCommand(cfg, args[1:], stdout, stderr)
	}
	switch args[0] {
	case "cache":
		return cacheCommand(cfg, args[1:], stdout, stderr)
	case "dash":
		return dashCommand(cfg, args[1:], stdout, stderr)
	case "cancel", "reprio", "pause", "resume":
		return controlCommand(cfg, args[0], args[1:], stdout, stderr)
	}
	if p, err := strconv.Atoi(args[0]); err != nil || p < 1 || p > 9 {
		fmt.Fprintf(stderr, "heavyq: first argument must be a priority 1-9 or a command (status, dash, history, cache, hook, cancel, reprio, pause, resume, version)\n\n%s", usage)
		return ExitUsage
	}
	return runCommand(cfg, args, stderr)
}

// version is the ldflags Version, else the module version recorded by
// `go install module@vX.Y.Z`.
func version() string {
	if Version != "dev" {
		return Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return strings.TrimPrefix(bi.Main.Version, "v")
	}
	return Version
}

// QueueDir is $HEAVYQ_DIR, else $XDG_RUNTIME_DIR/heavyq, else <tmp>/heavyq-<uid>.
func QueueDir() string {
	if d := os.Getenv("HEAVYQ_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "heavyq")
	}
	return filepath.Join(os.TempDir(), "heavyq-"+strconv.Itoa(os.Getuid()))
}

// localSlotPaths returns the lock files of the local heavy or light slots.
func localSlotPaths(cfg *config.Config, light bool) []string {
	if !light && len(cfg.Local.LegacyLockFiles) > 0 {
		return cfg.Local.LegacyLockFiles
	}
	n, kind := cfg.Local.HeavySlots, "heavy"
	if light {
		n, kind = cfg.Local.LightSlots, "light"
	}
	out := make([]string, n)
	for i := range out {
		out[i] = filepath.Join(QueueDir(), "slots", fmt.Sprintf("%s-%d.lock", kind, i+1))
	}
	return out
}

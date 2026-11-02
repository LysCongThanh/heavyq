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

// Main runs heavyq with args (without the program name) and returns the exit code.
// Commands are found through the registry (command_registry.go); there is no
// list of them here.
func Main(args []string, stdout, stderr io.Writer) int {
	cfgPath, explicit := config.DefaultPath(), os.Getenv("HEAVYQ_CONFIG") != ""
	if len(args) >= 2 && args[0] == "--config" {
		cfgPath, explicit, args = args[1], true, args[2:]
	}
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText())
		return ExitUsage
	}
	// heavyq <command> --help: the command's own usage block, before any config is read.
	if c, ok := wantsCommandHelp(args); ok {
		writeCommandHelp(stdout, c)
		return 0
	}
	c, ok := lookupCommand(args[0])
	if !ok {
		fmt.Fprint(stderr, unknownCommandMessage())
		return ExitUsage
	}
	return dispatch(c, cfgPath, explicit, args, stdout, stderr)
}

// versionCommand prints the version: `heavyq version` and `heavyq --version`.
func versionCommand(stdout io.Writer) int {
	fmt.Fprintln(stdout, "heavyq", version())
	return 0
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

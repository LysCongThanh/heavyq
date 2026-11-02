package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// Commands register themselves. A command lives in <name>_command.go (its
// handler) and <name>_command_spec.go (one init() that calls registerCommand
// with its table entry and a Run adapter). Nothing else lists commands: Main
// dispatches through the registry, and the usage text, `--help`, completions
// and the man page are generated from it. A new command adds files and edits
// none that exist.

// configNeed says what Main prepares before a command's Run.
type configNeed int

const (
	cfgNone     configNeed = iota // the command never reads the config
	cfgPathOnly                   // Run gets the path and loads it itself (hook, mentor)
	cfgLoaded                     // Main loads it first; a bad config is a usage error
)

// invocation is what Main hands to a command's Run.
type invocation struct {
	CfgPath  string
	Explicit bool           // the path came from --config or $HEAVYQ_CONFIG
	Cfg      *config.Config // set when the spec says cfgLoaded
	Argv     []string       // as typed after --config: Argv[0] is the command word (or the priority)
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
}

// args are the words after the command word.
func (inv *invocation) args() []string { return inv.Argv[1:] }

// registered is the command table, sorted by Order, then Name. It is filled by
// init() functions only and read-only afterwards.
var registered []commandSpec

// commandTable lists every command in the order of the usage text.
func commandTable() []commandSpec { return registered }

// registerCommand adds a command to the table. A malformed or duplicate entry
// is a programming error and stops the program at start, so the tests catch it.
func registerCommand(c commandSpec) {
	if c.Name == "" || c.Summary == "" || len(c.Synopsis) == 0 || c.Run == nil {
		panic(fmt.Sprintf("cli: command %q needs a name, summary, synopsis and Run", c.Name))
	}
	for _, o := range registered {
		if o.Name == c.Name {
			panic(fmt.Sprintf("cli: command %q registered twice", c.Name))
		}
	}
	registered = append(registered, c)
	sort.SliceStable(registered, func(i, j int) bool {
		if registered[i].Order != registered[j].Order {
			return registered[i].Order < registered[j].Order
		}
		return registered[i].Name < registered[j].Name
	})
}

// lookupCommand resolves the first argument of a run: a command name, an alias
// ("--help"), or a priority 1-9, which is the "run" command.
func lookupCommand(word string) (commandSpec, bool) {
	for _, c := range registered {
		if c.Name == word && c.Name != "run" { // "run" is spelled by its priority, never by name
			return c, true
		}
		for _, a := range c.Aliases {
			if a == word {
				return c, true
			}
		}
	}
	if p, err := strconv.Atoi(word); err == nil && p >= 1 && p <= 9 {
		return commandByName("run")
	}
	return commandSpec{}, false
}

// dispatch runs a command. One that wants the config gets it loaded first; a
// config that does not load is a usage error.
func dispatch(c commandSpec, cfgPath string, explicit bool, args []string, stdout, stderr io.Writer) int {
	inv := &invocation{CfgPath: cfgPath, Explicit: explicit, Argv: args, Stdin: os.Stdin, Stdout: stdout, Stderr: stderr}
	if c.Config == cfgLoaded {
		cfg, err := config.Load(cfgPath, explicit)
		if err != nil {
			fmt.Fprintln(stderr, "heavyq:", err)
			return ExitUsage
		}
		inv.Cfg = cfg
	}
	return c.Run(inv)
}

// unknownCommandMessage is the usage error for a first argument that is none of them.
func unknownCommandMessage() string {
	return fmt.Sprintf("heavyq: first argument must be a priority 1-9 or a command (%s)\n\n%s",
		strings.Join(commandNames(), ", "), usageText())
}

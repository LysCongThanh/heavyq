package cli

import "strings"

// The command table is the one description of heavyq's command line that the
// generated surfaces read: `<cmd> --help` blocks, shell completions
// (`heavyq completion`) and the man page. The dispatch in Main and the flag
// parsers stay where they are; command_table_drift_test.go fails when the table
// and the code or the usage text disagree.

// flagSpec describes one flag.
type flagSpec struct {
	Long    string   // "json" for --json ("" when there is only a short form)
	Short   string   // "v" for -v ("" when there is none)
	Value   string   // placeholder of the value ("DUR"); "" for a switch
	Help    string   // one line
	Choices []string // completion candidates of the value
	Path    string   // "file" or "dir" when the value is a path
}

// subSpec is one subcommand of a command that has some.
type subSpec struct {
	Name     string
	Args     string // what follows the name in the synopsis, e.g. "[name]"
	Summary  string // one line
	Flags    []flagSpec
	NextWord []string // fixed words that may follow (install claude)
}

// commandSpec is one first-argument command.
type commandSpec struct {
	Name     string // first argument; "run" stands for the <prio 1-9> form
	Summary  string // one line
	Synopsis []string
	Flags    []flagSpec // flags of the command itself, or shared by all its subcommands
	Subs     []subSpec
	NextWord []string // fixed words that may follow the command (completion bash|zsh|fish)
	// NextCommands: every command name may follow too (help <command>).
	NextCommands bool
	Notes        string // paragraph after the flags
	Docs         string // file under docs/ that has more
}

// configFlag is the global --config FILE that precedes every command.
var configFlag = flagSpec{Long: "config", Value: "FILE", Path: "file",
	Help: "config file (default: $HEAVYQ_CONFIG, else ~/.config/heavyq/config.toml)"}

// commandByName returns the spec of a first argument; a priority 1-9 is "run".
func commandByName(name string) (commandSpec, bool) {
	if len(name) == 1 && name[0] >= '1' && name[0] <= '9' {
		name = "run"
	}
	for _, c := range commandTable {
		if c.Name == name {
			return c, true
		}
	}
	return commandSpec{}, false
}

// commandNames lists the first-argument commands in table order, "run" excluded.
func commandNames() []string {
	var out []string
	for _, c := range commandTable {
		if c.Name != "run" {
			out = append(out, c.Name)
		}
	}
	return out
}

// display renders the flag as written on a command line: "-v, --verbose NAME".
func (f flagSpec) display() string {
	var parts []string
	if f.Short != "" {
		parts = append(parts, "-"+f.Short)
	}
	if f.Long != "" {
		parts = append(parts, "--"+f.Long)
	}
	s := strings.Join(parts, ", ")
	if f.Value != "" {
		s += " " + f.Value
	}
	return s
}

// words returns every spelling of the flag for completion ("--verbose", "-v").
func (f flagSpec) words() []string {
	var out []string
	if f.Long != "" {
		out = append(out, "--"+f.Long)
	}
	if f.Short != "" {
		out = append(out, "-"+f.Short)
	}
	return out
}

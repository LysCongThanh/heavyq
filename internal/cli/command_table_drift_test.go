package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// These tests keep the command table (which feeds --help, completions and the man page) in step with the
// code and the usage text. When one fails, update command_table_data.go / command_table_more.go.

// tableFix tells a contributor what to do when a drift test fails.
const tableFix = "\nTo fix: add or update the command, subcommand or flag in the command table " +
	"(internal/cli/command_table_data.go for queue/gate/item commands, command_table_more.go for the rest: " +
	"Name, Summary, Synopsis, Flags, Subs) and in the `usage` text in internal/cli/app.go, " +
	"then run `go test ./internal/cli -run 'Table|Completion|Help|Man'`. " +
	"If the change touches a config key or the status snapshot, also run `cd tools/schemagen && go run .` and commit schema/."

// generatedSources are the files that describe commands for the table; their literals do not count as code.
func isTableOrGeneratedFile(name string) bool {
	for _, p := range []string{"command_table", "command_help", "completion", "man_page"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// cliSources returns the text of every non-test, non-generated source file of the package, by name.
func cliSources(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || isTableOrGeneratedFile(f) {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		out[f] = string(b)
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestTableCommandsMatchUsage: the commands of the usage text and of the table are the same set.
func TestTableCommandsMatchUsage(t *testing.T) {
	fromUsage := map[string]bool{}
	for _, line := range strings.Split(usage, "\n") {
		rest, ok := strings.CutPrefix(line, "  heavyq ")
		if !ok {
			continue
		}
		rest = strings.TrimPrefix(rest, "[--config FILE] ")
		first := strings.Fields(rest)[0]
		if strings.HasPrefix(first, "<prio") {
			first = "run"
		}
		for _, n := range strings.Split(first, "|") {
			fromUsage[n] = true
		}
	}
	fromTable := map[string]bool{}
	for _, c := range commandTable {
		fromTable[c.Name] = true
	}
	if a, b := strings.Join(sortedKeys(fromUsage), " "), strings.Join(sortedKeys(fromTable), " "); a != b {
		t.Errorf("usage lists:  %s\ntable lists:  %s%s", a, b, tableFix)
	}
}

// TestTableCommandsAreDispatched: every table command is a case of Main (run is the numeric fallthrough).
func TestTableCommandsAreDispatched(t *testing.T) {
	src := cliSources(t)["app.go"]
	for _, c := range commandTable {
		if c.Name != "run" && !strings.Contains(src, `"`+c.Name+`"`) {
			t.Errorf("command %q is in the table but Main does not dispatch it%s", c.Name, tableFix)
		}
	}
}

// TestTableSubcommandsExist: a subcommand is named in its synopsis and in the code that dispatches it.
func TestTableSubcommandsExist(t *testing.T) {
	all := ""
	for _, s := range cliSources(t) {
		all += s
	}
	for _, c := range commandTable {
		for _, s := range c.Subs {
			if !strings.Contains(strings.Join(c.Synopsis, "\n"), s.Name) {
				t.Errorf("%s %s: not in the synopsis%s", c.Name, s.Name, tableFix)
			}
			if !strings.Contains(all, `"`+s.Name+`"`) {
				t.Errorf("%s %s: no such subcommand in the code%s", c.Name, s.Name, tableFix)
			}
		}
	}
}

var (
	flagSetDef  = regexp.MustCompile(`\b(?:fs|flags)\.(?:Bool|String|Int|Duration|Float64)\("([a-z][a-z-]*)"`)
	runFlagName = regexp.MustCompile(`"-{1,2}([a-z][a-z-]*)"`)
)

// TestTableFlagsMatchCode: the flags the code defines (flag sets, and the run flags parsed by hand) are
// exactly the flags of the table, so a new flag cannot miss --help, the completions or the man page.
func TestTableFlagsMatchCode(t *testing.T) {
	inCode := map[string]bool{}
	for name, src := range cliSources(t) {
		for _, m := range flagSetDef.FindAllStringSubmatch(src, -1) {
			inCode[m[1]] = true
		}
		if name == "run_options.go" {
			for _, m := range runFlagName.FindAllStringSubmatch(src, -1) {
				inCode[m[1]] = true
			}
		}
	}
	inTable := map[string]bool{}
	add := func(flags []flagSpec) {
		for _, f := range flags {
			for _, n := range []string{f.Long, f.Short} {
				if n != "" {
					inTable[n] = true
				}
			}
		}
	}
	for _, c := range commandTable {
		add(c.Flags)
		for _, s := range c.Subs {
			add(s.Flags)
		}
	}
	for n := range inCode {
		if !inTable[n] {
			t.Errorf("flag %q is defined in the code but missing from the command table%s", n, tableFix)
		}
	}
	for n := range inTable {
		if !inCode[n] {
			t.Errorf("flag %q is in the command table but no code defines it%s", n, tableFix)
		}
	}
}

// TestUsageMentionsEveryRunFlag: the run flags of the table appear in the usage text's flag list.
func TestUsageMentionsEveryRunFlag(t *testing.T) {
	run, _ := commandByName("run")
	for _, f := range run.Flags {
		if f.Long != "" && !strings.Contains(usage, "--"+f.Long) {
			t.Errorf("usage does not mention --%s%s", f.Long, tableFix)
		}
	}
}

// TestTableIsWellFormed: no empty names, summaries or synopses; flags have a spelling and a help line.
func TestTableIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	check := func(where string, flags []flagSpec) {
		for _, f := range flags {
			if f.Long == "" && f.Short == "" {
				t.Errorf("%s: a flag has no name", where)
			}
			if f.Help == "" {
				t.Errorf("%s: flag %s has no help", where, f.display())
			}
		}
	}
	for _, c := range commandTable {
		if c.Name == "" || c.Summary == "" || len(c.Synopsis) == 0 {
			t.Errorf("command %q: name, summary and synopsis are required", c.Name)
		}
		if seen[c.Name] {
			t.Errorf("command %q is listed twice", c.Name)
		}
		seen[c.Name] = true
		check(c.Name, c.Flags)
		for _, s := range c.Subs {
			if s.Name == "" || s.Summary == "" {
				t.Errorf("%s: a subcommand needs a name and a summary", c.Name)
			}
			check(c.Name+" "+s.Name, s.Flags)
		}
	}
}

package hook

import (
	"fmt"
	"strings"
)

// Entry is one hook registration heavyq writes into a Claude Code settings
// file: an event, an optional matcher, the command, and its timeout in seconds.
type Entry struct {
	Event   string
	Matcher string // "" = no matcher
	Command string
	Timeout int
}

// hookSpec is an Entry before the binary is known: the subcommand that follows
// "<binary> hook ".
type hookSpec struct {
	Event   string
	Matcher string
	Sub     string
	Timeout int
}

// Feature is a named group of hooks `heavyq hook install claude --features`
// can install. A new hook (PostToolUse, UserPromptSubmit, ...) is one more row
// here and its handler in internal/cli.
type Feature struct {
	Name    string
	Summary string
	specs   []hookSpec
}

// stopFamilyTimeoutSecs bounds the SessionStart and stop-family hooks. They run
// under a 3 to 5 second budget of their own (fail open); this only caps a stall.
const stopFamilyTimeoutSecs = 10

// sessionStartMatcher: a new session, a resumed one, and after compaction (the
// moment context is lost). /clear and fork start from nothing on purpose.
const sessionStartMatcher = "startup|resume|compact"

// Features lists every installable feature, in install order.
var Features = []Feature{
	{Name: "queue", Summary: "PreToolUse: wrap heavy commands in the heavyq queue (the default)",
		specs: []hookSpec{{"PreToolUse", "Bash", "claude-pretooluse", hookTimeoutSecs}}},
	{Name: "brief", Summary: "SessionStart: the goal, your items and the next command, again after compaction",
		specs: []hookSpec{{"SessionStart", sessionStartMatcher, "claude-sessionstart", stopFamilyTimeoutSecs}}},
	{Name: "stop", Summary: "Stop, SubagentStop, TaskCompleted, TeammateIdle: the stop gate (acts only when `heavyq steer` is guard or destroy)",
		specs: []hookSpec{
			{"Stop", "", "claude-stop", stopFamilyTimeoutSecs},
			{"SubagentStop", "", "claude-stop SubagentStop", stopFamilyTimeoutSecs},
			{"TaskCompleted", "", "claude-stop TaskCompleted", stopFamilyTimeoutSecs},
			{"TeammateIdle", "", "claude-stop TeammateIdle", stopFamilyTimeoutSecs},
		}},
	{Name: "stance", Summary: "UserPromptSubmit: switch the lane into a stance on a keyword (or evidence) and inject it once; the brief carries it after compaction",
		specs: []hookSpec{{"UserPromptSubmit", "", "claude-userpromptsubmit", stopFamilyTimeoutSecs}}},
}

// DefaultFeatures is what install does without --features: the queue hook only.
var DefaultFeatures = []string{"queue"}

// ParseFeatures turns a comma list into feature names in table order, without
// duplicates. An unknown name is refused with the list of known ones.
func ParseFeatures(list string) ([]string, error) {
	want := map[string]bool{}
	for _, n := range strings.Split(list, ",") {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if _, ok := FindFeature(n); !ok {
			return nil, fmt.Errorf("unknown feature %q (known: %s)", n, FeatureNames())
		}
		want[n] = true
	}
	if len(want) == 0 {
		return nil, fmt.Errorf("--features is empty (known: %s)", FeatureNames())
	}
	var out []string
	for _, f := range Features {
		if want[f.Name] {
			out = append(out, f.Name)
		}
	}
	return out, nil
}

// FindFeature looks a feature up by name.
func FindFeature(name string) (Feature, bool) {
	for _, f := range Features {
		if f.Name == name {
			return f, true
		}
	}
	return Feature{}, false
}

// FeatureNames is the comma list of known features.
func FeatureNames() string {
	names := make([]string, len(Features))
	for i, f := range Features {
		names[i] = f.Name
	}
	return strings.Join(names, ", ")
}

// EntriesFor builds the registrations of features, each calling bin (already
// shell-quoted) as `<bin> hook <subcommand>`.
func EntriesFor(bin string, features []string) []Entry {
	var out []Entry
	for _, name := range features {
		f, ok := FindFeature(name)
		if !ok {
			continue
		}
		for _, s := range f.specs {
			out = append(out, Entry{Event: s.Event, Matcher: s.Matcher, Command: bin + " hook " + s.Sub, Timeout: s.Timeout})
		}
	}
	return out
}

// ownsSubcommand reports whether name (`claude-stop`) is a hook some feature
// installs. A heavyq hook the installer does not manage yet (one a user wrote
// by hand) is left alone by install and uninstall.
func ownsSubcommand(name string) bool {
	for _, f := range Features {
		for _, s := range f.specs {
			if strings.Fields(s.Sub)[0] == name {
				return true
			}
		}
	}
	return false
}

// managedEvents is every event some feature registers: the ones install and
// uninstall look at.
func managedEvents() map[string]bool {
	m := map[string]bool{}
	for _, f := range Features {
		for _, s := range f.specs {
			m[s.Event] = true
		}
	}
	return m
}

// ownedSubcommand returns the heavyq hook subcommand a settings command runs
// ("claude-stop"), when the command is exactly `<binary> hook claude-x` with
// at most one more word (the event). The plugin's `sh -c '... || exit 0'`
// wrapper does not match, which keeps the two apart.
func ownedSubcommand(cmd string) (string, bool) {
	f := strings.Fields(cmd)
	// "hook" is the second or the third word from the end: `hook claude-x` or
	// `hook claude-x EVENT`.
	for k := len(f) - 2; k >= 1 && k >= len(f)-3; k-- {
		if f[k] == "hook" && strings.HasPrefix(f[k+1], "claude-") {
			return f[k+1], true
		}
	}
	return "", false
}

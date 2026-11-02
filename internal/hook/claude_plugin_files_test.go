package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The CI stand-in for `claude plugin validate`: the repository files are parsed and checked against the
// schema documented at https://code.claude.com/docs/en/plugins/components.md and marketplace-reference.md.

func repoFile(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func decodeJSON(t *testing.T, rel string, v any) {
	t.Helper()
	if err := json.Unmarshal(repoFile(t, rel), v); err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
}

// Plugin and marketplace names: kebab-case, no reserved Anthropic prefixes.
var kebab = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func TestPluginMarketplaceManifest(t *testing.T) {
	var m struct {
		Name  string `json:"name"`
		Owner struct {
			Name string `json:"name"`
		} `json:"owner"`
		Plugins []struct {
			Name        string `json:"name"`
			Source      string `json:"source"`
			Description string `json:"description"`
		} `json:"plugins"`
	}
	decodeJSON(t, ".claude-plugin/marketplace.json", &m)
	if m.Name != "heavyq" || !kebab.MatchString(m.Name) {
		t.Errorf("marketplace name = %q, want heavyq (the install id is heavyq@heavyq)", m.Name)
	}
	if m.Owner.Name == "" {
		t.Error("marketplace owner.name is required")
	}
	if len(m.Plugins) != 1 {
		t.Fatalf("want exactly one plugin entry, got %d", len(m.Plugins))
	}
	p := m.Plugins[0]
	if p.Name != "heavyq" || p.Source != "./plugins/heavyq" || p.Description == "" {
		t.Errorf("plugin entry = %+v", p)
	}
	if _, err := os.Stat(filepath.Join("..", "..", filepath.FromSlash(p.Source), ".claude-plugin", "plugin.json")); err != nil {
		t.Errorf("source %s has no plugin.json: %v", p.Source, err)
	}
	if PluginID != m.Plugins[0].Name+"@"+m.Name {
		t.Errorf("PluginID %q does not match the marketplace (%s@%s)", PluginID, m.Plugins[0].Name, m.Name)
	}
}

func TestPluginManifest(t *testing.T) {
	var p struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Author      struct {
			Name string `json:"name"`
		} `json:"author"`
		Homepage string `json:"homepage"`
		License  string `json:"license"`
	}
	decodeJSON(t, "plugins/heavyq/.claude-plugin/plugin.json", &p)
	if p.Name != "heavyq" || !kebab.MatchString(p.Name) {
		t.Errorf("plugin name = %q", p.Name)
	}
	if p.Description == "" || p.Author.Name == "" || !strings.HasPrefix(p.Homepage, "https://") || p.License != "MIT" {
		t.Errorf("incomplete manifest: %+v", p)
	}
}

// pluginCommand is the command the plugin runs for a heavyq hook subcommand (what follows "heavyq hook ").
func pluginCommand(sub string) string {
	return "sh -c 'command -v heavyq >/dev/null 2>&1 && exec heavyq hook " + sub + " || exit 0'"
}

// The plugin carries every installable feature: hooks.json must say exactly what the Features table says
// (event, matcher, timeout, subcommand), wrapped in the command -v guard.
func TestPluginHooksFile(t *testing.T) {
	var f struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	decodeJSON(t, "plugins/heavyq/hooks/hooks.json", &f)
	want := 0
	for _, feat := range Features {
		for _, s := range feat.specs {
			want++
			groups := f.Hooks[s.Event]
			if len(groups) != 1 || len(groups[0].Hooks) != 1 {
				t.Errorf("%s: want exactly one group with one hook (under a top-level \"hooks\" key), got %+v", s.Event, groups)
				continue
			}
			g, h := groups[0], groups[0].Hooks[0]
			if g.Matcher != s.Matcher {
				t.Errorf("%s: matcher %q, want %q", s.Event, g.Matcher, s.Matcher)
			}
			if h.Type != "command" || h.Command != pluginCommand(s.Sub) || h.Timeout != s.Timeout {
				t.Errorf("%s: hook = %+v, want command %q with timeout %d", s.Event, h, pluginCommand(s.Sub), s.Timeout)
			}
			if isOurs(map[string]any{"command": h.Command}) {
				t.Errorf("%s: the plugin command must not look like a settings-installed entry: uninstall would remove it from nowhere, and tests would confuse the two", s.Event)
			}
		}
	}
	if len(f.Hooks) != want {
		t.Errorf("hooks.json has %d events, the Features table %d", len(f.Hooks), want)
	}
	if !strings.Contains(pluginCommand(Subcommand[len("hook "):]), "heavyq "+Subcommand) {
		t.Error("the queue command no longer runs `heavyq " + Subcommand + "`")
	}
}

func TestPluginDocsQuoteTheHookCommands(t *testing.T) {
	docs := string(repoFile(t, "docs/claude-code.md"))
	for _, feat := range Features {
		for _, s := range feat.specs {
			if !strings.Contains(docs, pluginCommand(s.Sub)) {
				t.Errorf("docs/claude-code.md must document the plugin hook command verbatim: %s", pluginCommand(s.Sub))
			}
		}
	}
}

func TestPluginSkill(t *testing.T) {
	src := string(repoFile(t, "plugins/heavyq/skills/heavyq/SKILL.md"))
	if !strings.HasPrefix(src, "---\nname: heavyq\ndescription: ") {
		t.Fatal("SKILL.md must open with frontmatter: name first, description second")
	}
	front, body, ok := strings.Cut(strings.TrimPrefix(src, "---\n"), "\n---\n")
	if !ok {
		t.Fatal("SKILL.md frontmatter is not closed")
	}
	desc := strings.TrimPrefix(strings.SplitN(front, "\n", 2)[1], "description: ")
	if n := len(desc); n == 0 || n >= 1024 {
		t.Errorf("description is %d bytes, want 1..1023", n)
	}
	for _, want := range []string{"heavyq gate status", "heavyq gate run", "heavyq why", "heavyq help agents", "75", "78", "bypass"} {
		if !strings.Contains(desc+body, want) {
			t.Errorf("SKILL.md does not mention %q", want)
		}
	}
	if n := strings.Count(src, "\n"); n > 60 {
		t.Errorf("SKILL.md is %d lines, want at most 60", n)
	}
}

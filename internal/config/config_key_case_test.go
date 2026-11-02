package config

import (
	"strings"
	"testing"
)

// The TOML library matches keys to struct fields ignoring case; the user
// config must not let that through: "[LOCAL]" next to "[local]" merged into
// one table and which value won was undefined.
func TestLoadRejectsCaseVariantKeys(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"upper case table", "[Queue]\npoll = \"3s\"\n", `:1: unknown key "Queue"`},
		{"all caps local", "[LOCAL]\nheavy_slots = 4\n", `:1: unknown key "LOCAL"`},
		{"upper case key", "[local]\nHeavy_Slots = 3\n", `:2: unknown key "local.Heavy_Slots"`},
		{"upper case remote array", "[[REMOTE]]\nname = \"a\"\nssh = \"a\"\n", `:1: unknown key "REMOTE"`},
		{"upper case remote key", "[[remote]]\nname = \"a\"\nSSH = \"a\"\n", `:3: unknown key "remote.SSH"`},
		{"upper case nested hook rule", "[hook]\npermission = \"allow\"\n[[hook.Rule]]\nmatch = [\"bun test\"]\n", `:3: unknown key "hook.Rule"`},
		{"case twin of local", "[LOCAL]\nheavy_slots = 1\n\n[local]\nheavy_slots = 4\n", `:1: unknown key "LOCAL"`},
		{"case twin after local", "[local]\nheavy_slots = 4\n\n[Local]\nheavy_slots = 1\n", `:4: unknown key "Local"`},
		{"case twin of remote", "[[remote]]\nname = \"a\"\nssh = \"a\"\n[[Remote]]\nname = \"b\"\nssh = \"b\"\n", `:4: unknown key "Remote"`},
		{"upper case cache key", "[cache]\nMax_Size_MB = 10\n", `:2: unknown key "cache.Max_Size_MB"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := writeConfig(t, c.body)
			_, err := Load(p, true)
			if err == nil {
				t.Fatalf("accepted %q", c.body)
			}
			if want := p + c.want; !strings.Contains(err.Error(), want) {
				t.Fatalf("error %q does not contain %q", err, want)
			}
		})
	}
}

// Lower-case configs, including every table and the array of tables, load
// exactly as before, and values (which may have any case) are untouched.
func TestLoadKeepsValidLowerCaseConfigs(t *testing.T) {
	c, err := Load(writeConfig(t, `
[queue]
base_ref = "Origin/Main"
[local]
heavy_slots = 4
[[remote]]
name = "Box-1"
ssh = "Box-1"
[[hook.rule]]
match = ["bun test"]
`), true)
	if err != nil {
		t.Fatal(err)
	}
	if c.Local.HeavySlots != 4 || c.Queue.BaseRef != "Origin/Main" {
		t.Fatalf("local/queue: %+v %+v", c.Local, c.Queue)
	}
	if r, ok := c.Machine("Box-1"); !ok || r.SSH != "Box-1" {
		t.Fatalf("remote: %+v", r)
	}
	if len(c.Hook.Rules) != 1 {
		t.Fatalf("hook rules: %+v", c.Hook.Rules)
	}
}

func TestLoadErrorsKeepTheirShape(t *testing.T) {
	p := writeConfig(t, "[queue\n")
	if _, err := Load(p, true); err == nil || !strings.Contains(err.Error(), "config "+p) {
		t.Fatalf("syntax error must name the file: %v", err)
	}
	p = writeConfig(t, "[local]\nheavy_slots = 100\n")
	if _, err := Load(p, true); err == nil || !strings.HasPrefix(err.Error(), p+": [local]:") {
		t.Fatalf("validation error must name the file and table: %v", err)
	}
}

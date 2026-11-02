package config

import "testing"

func TestRemoteEnvPath(t *testing.T) {
	c, err := Load(writeConfig(t, "[[remote]]\nname = \"mac\"\nssh = \"mac\"\nenv_path = \"~/.local/go-sdk/bin:/opt/homebrew/bin\"\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := c.Machine("mac"); !ok || r.EnvPath != "~/.local/go-sdk/bin:/opt/homebrew/bin" {
		t.Fatalf("env_path not loaded: %+v", r)
	}
	c, err = Load(writeConfig(t, "[[remote]]\nname = \"plain\"\nssh = \"plain\"\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := c.Machine("plain"); r.EnvPath != "" {
		t.Fatalf("env_path must default to empty, got %q", r.EnvPath)
	}
}

func TestRemoteEnvPathRejectsUnsafeValues(t *testing.T) {
	for name, v := range map[string]string{
		"quote":     `/a'b`,
		"double":    `/a"b`,
		"dollar":    `$HOME/bin`,
		"backtick":  "/a`id`",
		"backslash": `/a\b`,
		"newline":   "/a\\nb",
		"only sep":  "::",
	} {
		body := "[[remote]]\nname = \"mac\"\nssh = \"mac\"\nenv_path = \"" + v + "\"\n"
		if _, err := Load(writeConfig(t, body), true); err == nil {
			t.Errorf("%s: env_path %q must be refused", name, v)
		}
	}
}

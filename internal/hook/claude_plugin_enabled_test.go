package hook

import "testing"

func TestPluginEnabled(t *testing.T) {
	cases := []struct {
		name, settings string
		want           bool
	}{
		{"enabled", `{"enabledPlugins":{"heavyq@heavyq":true}}`, true},
		{"enabled among others", `{"model":"opus","enabledPlugins":{"a@b":false,"heavyq@heavyq":true}}`, true},
		{"disabled", `{"enabledPlugins":{"heavyq@heavyq":false}}`, false},
		{"other marketplace", `{"enabledPlugins":{"heavyq@elsewhere":true}}`, false},
		{"other plugin", `{"enabledPlugins":{"x@heavyq":true}}`, false},
		{"not a bool", `{"enabledPlugins":{"heavyq@heavyq":"true"}}`, false},
		{"version list value", `{"enabledPlugins":{"heavyq@heavyq":["1.0"]}}`, false},
		{"enabledPlugins is an array", `{"enabledPlugins":["heavyq@heavyq"]}`, false},
		{"no key", `{"hooks":{}}`, false},
		{"empty", ``, false},
		{"whitespace", " \n", false},
		{"not json", `{nope`, false},
		{"not an object", `[]`, false},
	}
	for _, c := range cases {
		if got := PluginEnabled([]byte(c.settings)); got != c.want {
			t.Errorf("%s: PluginEnabled(%s) = %v, want %v", c.name, c.settings, got, c.want)
		}
	}
}

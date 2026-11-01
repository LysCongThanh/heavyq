package agentmode

import (
	"strings"
	"testing"
)

func envOf(kv ...string) func(string) (string, bool) {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestResolve(t *testing.T) {
	for _, c := range []struct {
		name string
		env  []string
		want Mode
		warn string // substring, "" = none
	}{
		{"nothing set", nil, Human, ""},
		{"claude code", []string{"CLAUDECODE", "1"}, Agent, ""},
		{"claude code AI_AGENT only", []string{"AI_AGENT", "claude-code_2-1-316_agent"}, Agent, ""},
		{"gemini", []string{"GEMINI_CLI", "1"}, Agent, ""},
		{"copilot", []string{"COPILOT_CLI", "1"}, Agent, ""},
		{"codex sandbox", []string{"CODEX_SANDBOX", "seatbelt"}, Agent, ""},
		{"cursor", []string{"CURSOR_AGENT", "1"}, Agent, ""},
		{"empty value is not set", []string{"CLAUDECODE", ""}, Human, ""},
		{"zero value is not set", []string{"CLAUDECODE", "0"}, Human, ""},
		{"false value is not set", []string{"GEMINI_CLI", "false"}, Human, ""},
		{"a human's CODEX_HOME is not an agent", []string{"CODEX_HOME", "/x"}, Human, ""},
		{"HEAVYQ_AGENT=1 forces on", []string{"HEAVYQ_AGENT", "1"}, Agent, ""},
		{"HEAVYQ_AGENT=0 forces off", []string{"HEAVYQ_AGENT", "0", "CLAUDECODE", "1"}, Human, ""},
		{"HEAVYQ_AGENT=off", []string{"HEAVYQ_AGENT", "OFF", "CLAUDECODE", "1"}, Human, ""},
		{"HEAVYQ_OUTPUT=human beats detection", []string{"HEAVYQ_OUTPUT", "human", "CLAUDECODE", "1"}, Human, ""},
		{"HEAVYQ_OUTPUT=agent", []string{"HEAVYQ_OUTPUT", " Agent "}, Agent, ""},
		{"HEAVYQ_OUTPUT=json", []string{"HEAVYQ_OUTPUT", "json"}, JSON, ""},
		{"HEAVYQ_OUTPUT beats HEAVYQ_AGENT", []string{"HEAVYQ_OUTPUT", "json", "HEAVYQ_AGENT", "0"}, JSON, ""},
		{"bad HEAVYQ_OUTPUT falls through", []string{"HEAVYQ_OUTPUT", "xml", "CLAUDECODE", "1"}, Agent, "HEAVYQ_OUTPUT"},
		{"bad HEAVYQ_OUTPUT, then HEAVYQ_AGENT", []string{"HEAVYQ_OUTPUT", "xml", "HEAVYQ_AGENT", "0", "CLAUDECODE", "1"}, Human, "HEAVYQ_OUTPUT"},
		{"bad HEAVYQ_AGENT falls through", []string{"HEAVYQ_AGENT", "maybe"}, Human, "HEAVYQ_AGENT"},
	} {
		got, warn := Resolve(envOf(c.env...))
		if got != c.want {
			t.Errorf("%s: mode %v, want %v", c.name, got, c.want)
		}
		if (c.warn == "") != (warn == "") || !strings.Contains(warn, c.warn) {
			t.Errorf("%s: warn %q, want it to contain %q", c.name, warn, c.warn)
		}
	}
}

func TestBadValuesAreQuotedShort(t *testing.T) {
	_, warn := Resolve(envOf("HEAVYQ_OUTPUT", strings.Repeat("x", 500)))
	if len(warn) > 120 {
		t.Fatalf("warning is %d bytes: %q", len(warn), warn)
	}
}

func TestIsAgentVarCoversTheTable(t *testing.T) {
	for _, e := range AgentEnv {
		if !IsAgentVar(e.Name) {
			t.Errorf("%s not reported as an agent variable", e.Name)
		}
	}
	for _, n := range []string{"HEAVYQ_OUTPUT", "HEAVYQ_AGENT"} {
		if !IsAgentVar(n) {
			t.Errorf("%s not reported", n)
		}
	}
	if IsAgentVar("PATH") || IsAgentVar("CODEX_HOME") {
		t.Error("unrelated variables reported")
	}
}

func TestModeStrings(t *testing.T) {
	if Human.Terse() || !Agent.Terse() || !JSON.Terse() {
		t.Error("Terse: only Human is verbose")
	}
	if Human.String() != "human" || Agent.String() != "agent" || JSON.String() != "json" {
		t.Error("Mode.String")
	}
}

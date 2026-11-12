// Package agentmode is what heavyq says to a coding agent instead of a person:
// detection of who is calling, the bounded capture of a command's output, the
// result digest printed after it, and the usage card of `heavyq help agents`.
//
// The package is pure apart from the capture's log file: everything that
// depends on the environment is injected, so every budget can be tested.
package agentmode

import (
	"strings"
)

// Mode is how heavyq words its own output.
type Mode int

const (
	// Human is the default: progress every 30 s, sentences, nothing hidden.
	Human Mode = iota
	// Agent is terse: at most queued, running and the digest; no ANSI.
	Agent
	// JSON is Agent's chatter with the digest as a single JSON object.
	JSON
)

// Terse reports whether heavyq's own chatter is rate-limited.
func (m Mode) Terse() bool { return m != Human }

func (m Mode) String() string {
	switch m {
	case Agent:
		return "agent"
	case JSON:
		return "json"
	}
	return "human"
}

// AgentEnv is the table of environment variables that say "a coding agent
// started this command". One place, so adding an agent is one line.
//
// A variable counts when it is set to anything except "", "0" or "false".
// Sources: Claude Code, Gemini CLI and Copilot CLI set theirs in the shell
// they give the model (checked against Claude Code 2.1.316 and by the
// integration research). The Codex and Cursor names are what third-party
// detectors (laravel/agent-detector, is-ai-agent) use; their vendors do not
// document them, so they are best effort. A hookless agent that none of
// these catches sets HEAVYQ_OUTPUT=agent.
//
// Names that also configure a tool for humans (CODEX_HOME, CURSOR_TRACE_ID,
// CLAUDE_CONFIG_DIR) are deliberately absent: a human who set one is not an agent.
var AgentEnv = []struct{ Name, Agent string }{
	{"CLAUDECODE", "Claude Code"},
	{"AI_AGENT", "any agent that follows the AI_AGENT convention (Claude Code sets it too)"},
	{"GEMINI_CLI", "Gemini CLI"},
	{"COPILOT_CLI", "GitHub Copilot CLI"},
	{"CODEX_SANDBOX", "OpenAI Codex (undocumented)"},
	{"CODEX_CI", "OpenAI Codex (undocumented)"},
	{"CODEX_THREAD_ID", "OpenAI Codex (undocumented)"},
	{"CURSOR_AGENT", "Cursor agent (undocumented)"},
}

// IsAgentVar reports whether name is one heavyq reads to detect an agent, or
// one of its own output switches. Tests use it to give a child process a
// clean environment.
func IsAgentVar(name string) bool {
	if name == "HEAVYQ_OUTPUT" || name == "HEAVYQ_AGENT" {
		return true
	}
	for _, e := range AgentEnv {
		if e.Name == name {
			return true
		}
	}
	return false
}

// Resolve picks the output mode. lookup is os.LookupEnv. Precedence:
//
//  1. HEAVYQ_OUTPUT = agent | human | json
//  2. HEAVYQ_AGENT  = 1/true/yes/on (agent) or 0/false/no/off (human)
//  3. an agent variable of AgentEnv: agent
//  4. human
//
// An unusable value of HEAVYQ_OUTPUT or HEAVYQ_AGENT is not an error (it only
// shapes output) but is not ignored silently either: warn says what was
// ignored, and the next rule decides.
func Resolve(lookup func(string) (string, bool)) (mode Mode, warn string) {
	get := func(name string) string {
		v, _ := lookup(name)
		return strings.ToLower(strings.TrimSpace(v))
	}
	switch v := get("HEAVYQ_OUTPUT"); v {
	case "":
	case "agent":
		return Agent, ""
	case "human":
		return Human, ""
	case "json":
		return JSON, ""
	default:
		warn = "ignoring $HEAVYQ_OUTPUT=" + quoteShort(v) + " (want agent, human or json)"
	}
	switch v := get("HEAVYQ_AGENT"); v {
	case "":
	case "1", "true", "yes", "on":
		return Agent, warn
	case "0", "false", "no", "off":
		return Human, warn
	default:
		warn = joinWarn(warn, "ignoring $HEAVYQ_AGENT="+quoteShort(v)+" (want 1 or 0)")
	}
	if UnderAgent(lookup) {
		return Agent, warn
	}
	return Human, warn
}

// UnderAgent reports whether a coding agent's own variables (AgentEnv) are
// set, whatever HEAVYQ_OUTPUT or HEAVYQ_AGENT say: those choose how heavyq
// words its output, not who runs it, so a guard that keeps an agent from doing
// something must ask this, never Resolve.
func UnderAgent(lookup func(string) (string, bool)) bool {
	for _, e := range AgentEnv {
		v, _ := lookup(e.Name)
		if v = strings.ToLower(strings.TrimSpace(v)); v != "" && v != "0" && v != "false" {
			return true
		}
	}
	return false
}

func joinWarn(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

// quoteShort shows a bad value without letting it flood the warning.
func quoteShort(v string) string {
	if len(v) > 20 {
		v = strings.ToValidUTF8(v[:20], "") + "..."
	}
	return `"` + v + `"`
}

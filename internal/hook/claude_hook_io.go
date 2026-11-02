package hook

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/history"
)

// This file is the part of the Claude Code hook protocol that every heavyq
// hook except PreToolUse shares: reading the common input fields leniently, and
// building the output objects. A new hook (PostToolUse, UserPromptSubmit, ...)
// adds its own decision logic in internal/cli and reuses these.

// MaxContextChars keeps injected text under Claude Code's 10,000-character cap
// for additionalContext, which would otherwise turn it into a file path plus a
// preview.
const MaxContextChars = 9500

// Input is the part of a hook's stdin JSON heavyq reads, across events. A field
// of the wrong type is dropped (zero value), never an error: a hook's job is to
// say nothing when in doubt, not to fail.
type Input struct {
	SessionID            string // only when it is a valid tag (history.ValidTag)
	Cwd                  string
	Event                string // hook_event_name
	TranscriptPath       string
	Source               string // SessionStart: startup | resume | clear | compact | fork
	StopHookActive       bool   // Stop, SubagentStop
	LastAssistantMessage string // Stop, SubagentStop
	AgentID              string
	Prompt               string // UserPromptSubmit: what the user typed
}

// ParseInput reads the common fields. An error means stdin is not a JSON object.
func ParseInput(stdin []byte) (Input, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(stdin, &raw); err != nil {
		return Input{}, fmt.Errorf("stdin is not hook JSON: %w", err)
	}
	str := func(k string) string {
		var s string
		if json.Unmarshal(raw[k], &s) != nil {
			return ""
		}
		return s
	}
	in := Input{Cwd: str("cwd"), Event: str("hook_event_name"), TranscriptPath: str("transcript_path"),
		Source: str("source"), LastAssistantMessage: str("last_assistant_message"), AgentID: str("agent_id"), Prompt: str("prompt")}
	if id := str("session_id"); history.ValidTag(id) {
		in.SessionID = id
	}
	var active bool
	if json.Unmarshal(raw["stop_hook_active"], &active) == nil {
		in.StopHookActive = active
	}
	return in, nil
}

type contextOutput struct {
	Specific struct {
		Event   string `json:"hookEventName"`
		Context string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

// ContextOutput is `{"hookSpecificOutput":{"hookEventName":event,
// "additionalContext":text}}`: context for Claude. On SessionStart it is
// added before the first prompt; on Stop and SubagentStop it continues the
// conversation without an error notice; on PostToolUse it rides along with the
// tool result.
func ContextOutput(event, text string) ([]byte, error) {
	var o contextOutput
	o.Specific.Event, o.Specific.Context = event, ClipContext(text)
	return marshal(o)
}

// StopBlockOutput is `{"decision":"block","reason":...}`: Claude keeps working
// and is given the reason as its next instruction.
func StopBlockOutput(reason string) ([]byte, error) {
	return marshal(struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}{"block", ClipContext(reason)})
}

// SystemMessageOutput is `{"systemMessage":...}`: a note for the user, shown by
// Claude Code, with no effect on the agent.
func SystemMessageOutput(msg string) ([]byte, error) {
	return marshal(struct {
		Message string `json:"systemMessage"`
	}{ClipContext(msg)})
}

// ClipContext cuts text to MaxContextChars on a character boundary.
func ClipContext(s string) string {
	if len(s) <= MaxContextChars {
		return s
	}
	return strings.ToValidUTF8(s[:MaxContextChars], "") + "…"
}

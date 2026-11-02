package cli

import (
	"encoding/json"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/flow"
	"github.com/LysCongThanh/heavyq/internal/redact"
)

// Longest reason text kept: a status line shows one, not a transcript.
const maxFlowReasonChars = 120

// flowPayload is the event-specific part of the hook JSON the flow hook reads
// (https://code.claude.com/docs/en/hooks). Every field is optional: Claude Code
// adds and renames them between versions, and a hook must say nothing, not fail.
type flowPayload struct {
	NotificationType string          `json:"notification_type"` // Notification
	Message          string          `json:"message"`           // Notification
	ToolName         string          `json:"tool_name"`         // PermissionRequest
	ToolInput        json.RawMessage `json:"tool_input"`        // PermissionRequest
	Error            json.RawMessage `json:"error"`             // StopFailure: an enum word
	FromModel        string          `json:"from_model"`        // PostModelSwitch
	ToModel          string          `json:"to_model"`          // PostModelSwitch
}

// parseFlowPayload reads the payload leniently: a field of the wrong type is
// dropped, never an error.
func parseFlowPayload(raw []byte) flowPayload {
	var p flowPayload
	if json.Unmarshal(raw, &p) == nil {
		return p
	}
	// One wrongly typed field fails the whole decode: read field by field.
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return flowPayload{}
	}
	str := func(k string) string {
		var s string
		_ = json.Unmarshal(m[k], &s)
		return s
	}
	return flowPayload{NotificationType: str("notification_type"), Message: str("message"), ToolName: str("tool_name"),
		ToolInput: m["tool_input"], Error: m["error"], FromModel: str("from_model"), ToModel: str("to_model")}
}

// errorWord is the StopFailure error enum ("rate_limit"); "unknown" when the
// field is absent or not a string.
func (p flowPayload) errorWord() string {
	var s string
	if json.Unmarshal(p.Error, &s) != nil || s == "" {
		return "unknown"
	}
	return oneLine(s, 40)
}

// toolSummary is `Bash(npm run e2e)`: the tool and what it was asked to touch,
// command lines redacted. Without a recognisable argument, just the tool name.
func (p flowPayload) toolSummary() string {
	name := oneLine(p.ToolName, 40)
	if name == "" {
		name = "tool"
	}
	var in map[string]any
	if json.Unmarshal(p.ToolInput, &in) != nil {
		return name
	}
	for _, k := range []string{"command", "file_path", "path", "url", "pattern", "query"} {
		if v, ok := in[k].(string); ok && v != "" {
			// Every value goes through the redactor: a URL carries credentials as readily as a command.
			return name + "(" + oneLine(redact.Command(squash(v)), maxFlowReasonChars) + ")"
		}
	}
	return name
}

// flowReason is the one line shown for a notification: its type, then the
// message Claude Code gave (redacted; it may quote a command).
func flowReason(class, message string) string {
	msg := oneLine(redact.Command(squash(message)), maxFlowReasonChars)
	if msg == "" {
		return class
	}
	return class + ": " + msg
}

// notificationClass maps a Notification's type to a block class. ok is false
// for the types that need no person (auth_success, quota_auto_resume_*, ...).
func notificationClass(t string) (string, bool) {
	switch t {
	case flow.ClassPermissionPrompt, flow.ClassIdlePrompt, flow.ClassAgentNeedsInput:
		return t, true
	}
	return "", false
}

// squash turns every run of whitespace (newlines included) into one space, so a
// multi-line command or message stays one readable line.
func squash(s string) string { return strings.Join(strings.Fields(s), " ") }

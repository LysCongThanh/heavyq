package hook

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Events a Claude Code tool result arrives as. A Bash command that exits
// non-zero is reported as PostToolUseFailure, one that exits 0 (a pipeline into
// `tail` does, whatever heavyq returned) as PostToolUse. The adapter takes both.
const (
	EventPostToolUse        = "PostToolUse"
	EventPostToolUseFailure = "PostToolUseFailure"
)

// PostCall is the part of a PostToolUse / PostToolUseFailure payload the
// mentor reads. It carries no output of the command: facts come from the
// ledger, never from what the command printed.
type PostCall struct {
	Event   string // EventPostToolUse or EventPostToolUseFailure
	Session string // the Claude session id when it is a valid tag, else ""
	Cwd     string
	Command string // the Bash command as the tool ran it (rewritten or not, Claude decides)
	// Lane is the --lane of a heavyq invocation in Command ("" when the
	// command is not one, or does not name a lane).
	Lane string
}

type postInput struct {
	Cwd       string                     `json:"cwd"`
	SessionID json.RawMessage            `json:"session_id"`
	Event     string                     `json:"hook_event_name"`
	ToolName  string                     `json:"tool_name"`
	ToolInput map[string]json.RawMessage `json:"tool_input"`
}

// ParsePostToolUse reads a hook payload. It returns nil and the reason when
// the call is not for the mentor (another event, another tool), and an error
// when the payload is not hook JSON at all. A missing or odd command is not an
// error: the mentor can still match the lane's newest ledger record.
func ParsePostToolUse(stdin []byte) (*PostCall, string, error) {
	var in postInput
	if err := json.Unmarshal(stdin, &in); err != nil {
		return nil, "", fmt.Errorf("stdin is not hook JSON: %w", err)
	}
	if in.Event != EventPostToolUse && in.Event != EventPostToolUseFailure {
		return nil, "event " + in.Event, nil
	}
	if in.ToolName != "Bash" {
		return nil, "tool " + in.ToolName, nil
	}
	call := &PostCall{Event: in.Event, Cwd: in.Cwd, Session: sessionID(in.SessionID)}
	if raw, ok := in.ToolInput["command"]; ok {
		_ = json.Unmarshal(raw, &call.Command) // not a string: no command
	}
	call.Lane = heavyqLane(call.Command)
	return call, "", nil
}

// heavyqLane returns the value of --lane in a heavyq invocation of cmd, "" when
// cmd is not a safe shell form, does not call heavyq or names no lane.
func heavyqLane(cmd string) string {
	p, err := Parse(cmd)
	if err != nil {
		return ""
	}
	for i, w := range p.Words {
		if !IsHeavyq(w) {
			continue
		}
		for j := i + 1; j < len(p.Words) && p.Words[j] != "--"; j++ {
			if p.Words[j] == "--lane" && j+1 < len(p.Words) {
				return p.Words[j+1]
			}
		}
		return ""
	}
	return ""
}

// PostContext is the hook output that hands text to the agent next to the
// tool result: {"hookSpecificOutput":{"hookEventName":<event>,
// "additionalContext":<text>}}. Claude Code caps the text at 10,000 characters;
// the mentor's notes are one line.
func PostContext(event, text string) ([]byte, error) {
	if event != EventPostToolUse && event != EventPostToolUseFailure {
		return nil, errors.New("not a post-tool event: " + event)
	}
	out := struct {
		Specific struct {
			Event             string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}{}
	out.Specific.Event, out.Specific.AdditionalContext = event, text
	return marshal(out)
}

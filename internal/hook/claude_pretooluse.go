package hook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/queue"
)

// defaultToolTimeoutMS is the Bash tool timeout when the call sets none.
const defaultToolTimeoutMS = 120000

// Options configure Decide. Everything environment-dependent is injected so
// the decision is a pure function of its inputs.
type Options struct {
	Rules      []config.HookRule
	Permission string        // "" or "allow"
	MaxWait    time.Duration // --max-wait, also added to the tool timeout
	Binary     string        // how to call heavyq in the rewritten command
	Lane       string        // $HEAVYQ_LANE of the hook process ("" = derive)
	Active     bool          // $HEAVYQ_ACTIVE is set: never rewrite
	LaneFor    func(cwd string) string
	// SessionSink, when set, is told the Claude session id (already validated:
	// 1-80 of A-Za-z0-9._:-) and the lane of every command that gets wrapped.
	// The id travels out of band so the rewritten command stays the same in
	// every session, which keeps exact permission rules matching.
	SessionSink func(lane, sessionID string)
}

// claudeInput is the part of the PreToolUse stdin JSON the hook reads.
type claudeInput struct {
	Cwd string `json:"cwd"`
	// SessionID is kept raw: a value of the wrong type must only cost the
	// session label, never the rewrite.
	SessionID json.RawMessage            `json:"session_id"`
	Event     string                     `json:"hook_event_name"`
	ToolName  string                     `json:"tool_name"`
	ToolInput map[string]json.RawMessage `json:"tool_input"`
}

type claudeOutput struct {
	Specific specificOutput `json:"hookSpecificOutput"`
}

type specificOutput struct {
	Event             string                     `json:"hookEventName"`
	Decision          string                     `json:"permissionDecision,omitempty"`
	DecisionReason    string                     `json:"permissionDecisionReason,omitempty"`
	UpdatedInput      map[string]json.RawMessage `json:"updatedInput"`
	AdditionalContext string                     `json:"additionalContext,omitempty"`
}

// Decide returns the JSON to print, or nil plus the reason the command is
// left unchanged. An error means the input itself was unusable.
func Decide(stdin []byte, o Options) ([]byte, string, error) {
	var in claudeInput
	if err := json.Unmarshal(stdin, &in); err != nil {
		return nil, "", fmt.Errorf("stdin is not hook JSON: %w", err)
	}
	if in.Event != "" && in.Event != "PreToolUse" {
		return nil, "event " + in.Event, nil
	}
	if in.ToolName != "Bash" {
		return nil, "tool " + in.ToolName, nil
	}
	var cmd string
	if err := json.Unmarshal(in.ToolInput["command"], &cmd); err != nil {
		return nil, "", errors.New("tool_input.command is not a string")
	}
	if o.Active {
		return nil, "HEAVYQ_ACTIVE is set", nil
	}
	p, err := Parse(cmd)
	if err != nil {
		return nil, "not a safe form: " + err.Error(), nil
	}
	for _, w := range append(p.Words, p.Env...) {
		if IsHeavyq(w) || strings.HasPrefix(w, "HEAVYQ_ACTIVE=") {
			return nil, "already wrapped", nil
		}
	}
	rule, pat, ok := MatchRule(o.Rules, p.Words)
	if !ok {
		return nil, "no rule matches", nil
	}
	lane := rule.Lane
	if lane == "" {
		lane = o.Lane
	}
	if lane == "" && o.LaneFor != nil {
		lane = o.LaneFor(in.Cwd)
	}
	lane = queue.SanitizeLane(lane)

	prefix := []string{ShellQuote(o.Binary), fmt.Sprint(rule.Priority), "--lane", lane}
	if rule.Light {
		prefix = append(prefix, "--light")
	}
	if len(p.Env) > 0 {
		prefix = append(prefix, "--remote", "local") // assignments do not travel to remotes
	}
	if rule.Cache {
		prefix = append(prefix, "--cache")
	}
	wait := o.MaxWait.Round(time.Second)
	prefix = append(prefix, "--max-wait", fmt.Sprintf("%ds", int(wait.Seconds())))
	prefix = append(prefix, "--")
	if sid := sessionID(in.SessionID); sid != "" && o.SessionSink != nil {
		o.SessionSink(lane, sid)
	}
	newCmd := cmd[:p.ProgAt] + strings.Join(prefix, " ") + " " + cmd[p.ProgAt:]

	upd := make(map[string]json.RawMessage, len(in.ToolInput)+1)
	for k, v := range in.ToolInput {
		upd[k] = v
	}
	upd["command"], _ = marshal(newCmd)
	timeout := int64(defaultToolTimeoutMS)
	if raw, ok := in.ToolInput["timeout"]; ok {
		var t float64
		if json.Unmarshal(raw, &t) == nil && t > 0 {
			timeout = int64(t)
		}
	}
	upd["timeout"], _ = marshal(timeout + wait.Milliseconds())

	out := claudeOutput{Specific: specificOutput{Event: "PreToolUse", UpdatedInput: upd,
		AdditionalContext: fmt.Sprintf("heavyq: %q matched hook rule %q, so it ran through the shared heavyq queue "+
			"(prio %d, lane %s). Exit 75 means the queue stayed busy for %s: retry later, do not bypass heavyq.",
			strings.Join(p.Words, " "), pat, rule.Priority, lane, wait)}}
	if o.Permission == "allow" {
		out.Specific.Decision = "allow"
		out.Specific.DecisionReason = "heavyq hook: wrapped in the heavyq queue (hook.permission = allow)"
	}
	b, err := marshal(out)
	if err != nil {
		return nil, "", err
	}
	return b, "rewritten: " + newCmd, nil
}

// sessionID returns the Claude session id for the ledger, or "" when it is
// missing, not a string, or not a valid tag (1-80 of A-Za-z0-9._:-). Anything
// else is dropped (the command is still wrapped, just without a session).
func sessionID(raw json.RawMessage) string {
	var s string
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil || !history.ValidTag(s) {
		return ""
	}
	return s
}

// marshal encodes without HTML escaping so '&&' stays readable.
func marshal(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

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
	"github.com/LysCongThanh/heavyq/internal/redact"
)

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
	// ToolTimeouts are Claude Code's Bash tool timeout settings, so the
	// rewritten timeout follows its default and cap.
	ToolTimeouts ToolTimeouts
	// CacheEnv is cache.env: a rule's --cache is kept only when every typed
	// assignment is one of these (the cache key sees no other variable).
	CacheEnv []string
	// LookPath, when set, must find the program word on PATH; a word it
	// cannot find (an alias or a function in the agent's shell) stays with
	// the shell, since heavyq runs programs without one.
	LookPath func(file string) (string, error)
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

// LeftMatched starts the reason for a command a rule matched that the hook
// still leaves unchanged; the caller logs those, since the user expected a
// rewrite.
const LeftMatched = "matched a rule, left unchanged: "

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
	if name := heavyqEnvConflict(p.Env); name != "" {
		return nil, LeftMatched + "assignment to " + name + " would change heavyq itself", nil
	}
	if prog := p.Words[0]; o.LookPath != nil && !strings.Contains(prog, "/") {
		if _, err := o.LookPath(prog); err != nil {
			return nil, fmt.Sprintf("%sprogram %q is not on PATH (an alias or a shell function?)", LeftMatched, prog), nil
		}
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
	if rule.Cache && cacheSeesEnv(p.Env, o.CacheEnv) {
		prefix = append(prefix, "--cache")
	}
	// --max-wait counts whole seconds and 0 means forever: round up, at least 1s.
	wait := max(o.MaxWait.Truncate(time.Second), time.Second)
	if wait < o.MaxWait {
		wait += time.Second
	}
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
	timeout, capMS, capped := toolTimeout(in.ToolInput["timeout"], isTrue(in.ToolInput["run_in_background"]),
		wait, o.ToolTimeouts)
	if timeout > 0 {
		upd["timeout"], _ = marshal(timeout)
	}

	note := fmt.Sprintf("heavyq: %q matched hook rule %q, so it ran through the shared heavyq queue "+
		"(prio %d, lane %s). Exit 75 means the queue stayed busy for %s: retry later, do not bypass heavyq.",
		queue.TruncateCmd(p.Words), pat, rule.Priority, lane, wait)
	if capped {
		note += fmt.Sprintf(" The tool timeout is capped at %ds, so time spent queued comes out of the command's own time.",
			capMS/1000)
	}
	out := claudeOutput{Specific: specificOutput{Event: "PreToolUse", UpdatedInput: upd, AdditionalContext: note}}
	if o.Permission == "allow" && allowScopeOK(p, in.Cwd) {
		out.Specific.Decision = "allow"
		out.Specific.DecisionReason = "heavyq hook: wrapped in the heavyq queue (hook.permission = allow)"
	}
	b, err := marshal(out)
	if err != nil {
		return nil, "", err
	}
	return b, "rewritten: " + redact.Command(newCmd), nil
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

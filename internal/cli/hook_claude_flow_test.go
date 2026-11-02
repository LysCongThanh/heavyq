package cli

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/hook"
)

func TestFlowPayloadIsReadLeniently(t *testing.T) {
	p := parseFlowPayload([]byte(`{"notification_type":"idle_prompt","message":5,"tool_name":"Bash","tool_input":{"command":"npm run e2e"},"error":"rate_limit","to_model":"m"}`))
	if p.NotificationType != "idle_prompt" || p.Message != "" || p.ToModel != "m" || p.errorWord() != "rate_limit" || p.toolSummary() != "Bash(npm run e2e)" {
		t.Fatalf("one field of the wrong type costs that field only: %+v", p)
	}
	if got := parseFlowPayload([]byte(`not json`)); got.ToolName != "" || got.NotificationType != "" {
		t.Fatalf("%+v", got)
	}
	if w := (flowPayload{Error: []byte(`{"code":1}`)}).errorWord(); w != "unknown" {
		t.Errorf("a non-string error is unknown: %q", w)
	}
}

func TestFlowToolSummary(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`{"tool_name":"Bash","tool_input":{"command":"TOKEN=abc12345 make deploy"}}`, "Bash(TOKEN=*** make deploy)"},
		{`{"tool_name":"Edit","tool_input":{"file_path":"/w/app/main.go","old_string":"x"}}`, "Edit(/w/app/main.go)"},
		{`{"tool_name":"WebFetch","tool_input":{"url":"https://example.com/x"}}`, "WebFetch(https://example.com/x)"},
		{`{"tool_name":"Task","tool_input":{"prompt":"do things"}}`, "Task"},
		{`{"tool_input":{"command":"ls"}}`, "tool(ls)"},
		{`{"tool_name":"Bash","tool_input":"not an object"}`, "Bash"},
	} {
		if got := parseFlowPayload([]byte(c.in)).toolSummary(); got != c.want {
			t.Errorf("%s\n got  %q\n want %q", c.in, got, c.want)
		}
	}
	long := parseFlowPayload([]byte(`{"tool_name":"Bash","tool_input":{"command":"` + strings.Repeat("x", 500) + `"}}`)).toolSummary()
	if !strings.HasPrefix(long, "Bash(xxx") || len(long) > 4*maxFlowReasonChars {
		t.Errorf("a long command is clipped: %d bytes", len(long))
	}
}

func TestFlowReasonAndNotificationClass(t *testing.T) {
	if got := flowReason("idle_prompt", "  Claude is\nwaiting  "); got != "idle_prompt: Claude is waiting" {
		t.Errorf("%q", got)
	}
	if got := flowReason("idle_prompt", ""); got != "idle_prompt" {
		t.Errorf("%q", got)
	}
	for _, ty := range []string{"permission_prompt", "idle_prompt", "agent_needs_input"} {
		if c, ok := notificationClass(ty); !ok || c != ty {
			t.Errorf("%s: %q %v", ty, c, ok)
		}
	}
	for _, ty := range []string{"auth_success", "elicitation_dialog", "quota_auto_resume_fired", "agent_completed", ""} {
		if _, ok := notificationClass(ty); ok {
			t.Errorf("%s needs no person", ty)
		}
	}
}

// PostToolUse fires after every tool call of every agent. When nothing is blocked (the usual case) the handler must
// be one failed unlink: no git call, no config, no refresh.
func TestFlowPostToolUseFastPathCostsAlmostNothing(t *testing.T) {
	t.Setenv("HEAVYQ_HISTORY", t.TempDir()+"/history.jsonl")
	t.Setenv(flowOffEnv, "on")
	in := hook.Input{SessionID: "sess-1", Cwd: t.TempDir(), Event: "PostToolUse"}
	const n = 200
	durs := make([]time.Duration, n)
	for i := range durs {
		start := time.Now()
		reply, err := handleFlow(in, []byte(`{}`))
		durs[i] = time.Since(start)
		if err != nil || reply.After != nil || len(reply.Stdout) != 0 {
			t.Fatalf("nothing blocked, nothing to do: %+v %v", reply, err)
		}
	}
	sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
	if p50 := durs[n/2]; p50 > time.Millisecond {
		t.Errorf("median %v for the common PostToolUse, want well under 1 ms", p50)
	}
}

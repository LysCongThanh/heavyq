package hook

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// decideOut runs Decide and decodes the parts of its output these tests read.
func decideOut(t *testing.T, cmd string, o Options) (out struct {
	H struct {
		Decision string         `json:"permissionDecision"`
		U        map[string]any `json:"updatedInput"`
		Context  string         `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}, why string) {
	t.Helper()
	b, why, err := Decide(hookInput(cmd), o)
	if err != nil || b == nil {
		t.Fatalf("%q: not rewritten: why=%q err=%v", cmd, why, err)
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out, why
}

// An assignment that changes how heavyq itself is found or where it keeps its
// queue, slots, config and ledger must not be applied to heavyq: the hook
// leaves such a command unchanged.
func TestDecideLeavesAssignmentsThatChangeHeavyqItself(t *testing.T) {
	for _, cmd := range []string{
		"PATH=/usr/local/go/bin:/usr/bin:/bin go test ./...",
		"HOME=/tmp/cleanhome go test ./...",
		"TMPDIR=/tmp/gotmp go test ./...",
		"XDG_RUNTIME_DIR=/tmp/r npm test",
		"XDG_STATE_HOME=/tmp/s npm test",
		"XDG_CONFIG_HOME=/tmp/c npm test",
		"HEAVYQ_DIR=/tmp/q npm test",
		"HEAVYQ_CONFIG=/tmp/c.toml npm test",
		"CLAUDECODE= npm test",
		"CI=1 HOME=/tmp/h npm test",
	} {
		if got := rewritten(t, cmd, testOptions()); got != "" {
			t.Errorf("%q must be left unchanged, got %q", cmd, got)
		}
	}
	// Harmless assignments keep working.
	if got := rewritten(t, "CGO_ENABLED=0 go test ./...", testOptions()); !strings.Contains(got, "CGO_ENABLED=0 heavyq 4 ") {
		t.Errorf("CGO_ENABLED=0 must still be wrapped, got %q", got)
	}
}

// hook.permission = allow covers no more than a Claude Code prefix rule
// would: an unknown assignment or a cd out of the project gets no decision.
func TestDecidePermissionAllowOnlyForPlainShapes(t *testing.T) {
	o := testOptions()
	o.Permission = "allow"
	for cmd, want := range map[string]string{
		"go test ./...":                           "allow",
		"cd web && npm run lint":                  "allow",
		"cd /work/wt-api/web && npm test":         "allow",
		"CI=1 npm test":                           "allow",
		"NODE_ENV=test CGO_ENABLED=0 npm test":    "allow",
		"GOFLAGS=-toolexec=./x go test ./...":     "",
		"LD_PRELOAD=./x.so go test ./...":         "",
		"CI=1 BASH_ENV=./x npm test":              "",
		"cd /tmp/untrusted && make test":          "",
		"cd ../other && make test":                "",
		"cd web/../../other && make test":         "",
		"cd ~/x && make test":                     "",
		"cd /work/wt-api-evil && make test":       "",
		"cd -- && make test":                      "",
		"cd /work/wt-api/../x && npm test":        "",
		"cd .[.] && make test":                    "",
		"cd .[.]/.[.]/tmp/untrusted && make test": "",
		"cd .? && make test":                      "",
		"cd web/* && make test":                   "",
		"cd web && GOFLAGS=-race go test ./...":   "",
	} {
		out, _ := decideOut(t, cmd, o)
		if out.H.Decision != want {
			t.Errorf("%q: permissionDecision %q, want %q", cmd, out.H.Decision, want)
		}
	}
}

// A hook rule with cache = true must not add --cache when the agent typed an
// assignment the cache key does not see: the variant would replay a result
// it never produced.
func TestDecideCacheRuleSkipsTypedAssignments(t *testing.T) {
	o := testOptions()
	o.Rules = append([]config.HookRule{{Match: []string{"go test"}, Priority: 4, Cache: true}}, DefaultRules...)
	if got := rewritten(t, "go test ./...", o); !strings.Contains(got, " --cache ") {
		t.Fatalf("a plain matched command keeps --cache: %q", got)
	}
	for _, cmd := range []string{"GOFLAGS=-race go test ./...", "CGO_ENABLED=0 go test ./..."} {
		if got := rewritten(t, cmd, o); got == "" || strings.Contains(got, "--cache") {
			t.Errorf("%q: must be wrapped without --cache, got %q", cmd, got)
		}
	}
}

// Secrets in the command never reach hook.log (the why string) or the
// additionalContext, which is also bounded.
func TestDecideRedactsWhyAndContext(t *testing.T) {
	o := testOptions()
	_, why := decideOut(t, "DATABASE_PASSWORD=hunter2 go test ./... -args -api-key=abc123", o)
	if strings.Contains(why, "hunter2") || strings.Contains(why, "abc123") || !strings.HasPrefix(why, "rewritten: ") {
		t.Fatalf("why leaks a secret: %q", why)
	}
	out, _ := decideOut(t, "go test ./... -args -token=ghp_abcdefghijklmnopqrstuvwxyz0123456789", o)
	if strings.Contains(out.H.Context, "ghp_abcdef") {
		t.Fatalf("additionalContext leaks a token: %q", out.H.Context)
	}
	out, _ = decideOut(t, "go test ./... -run "+strings.Repeat("TestX", 2000), o)
	if len(out.H.Context) > 600 {
		t.Fatalf("additionalContext is %d bytes for a long command", len(out.H.Context))
	}
}

// A sub-second max_wait must never become --max-wait 0s, which heavyq reads
// as "wait forever".
func TestDecideSubSecondMaxWaitNeverMeansForever(t *testing.T) {
	o := testOptions()
	for _, w := range []time.Duration{time.Millisecond, 400 * time.Millisecond, 1400 * time.Millisecond} {
		o.MaxWait = w
		got := rewritten(t, "go test ./...", o)
		if strings.Contains(got, "--max-wait 0s") || !strings.Contains(got, "--max-wait ") {
			t.Errorf("max_wait %s: %q", w, got)
		}
		if w == 1400*time.Millisecond && !strings.Contains(got, "--max-wait 2s") {
			t.Errorf("max_wait %s must round up to 2s, got %q", w, got)
		}
	}
}

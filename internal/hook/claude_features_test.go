package hook

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const testBin = "'/opt/heavy q/heavyq'"

func allFeatures() []string {
	var names []string
	for _, f := range Features {
		names = append(names, f.Name)
	}
	return names
}

// hooksOf is the array of groups per event; an event whose value is not an array (settings may hold odd shapes
// of events heavyq does not manage) is left out.
func hooksOf(t *testing.T, b []byte) map[string][]any {
	t.Helper()
	var v struct {
		Hooks map[string]any `json:"hooks"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	out := map[string][]any{}
	for event, val := range v.Hooks {
		if arr, ok := val.([]any); ok {
			out[event] = arr
		}
	}
	return out
}

func events(t *testing.T, b []byte) []string {
	t.Helper()
	var out []string
	for e := range hooksOf(t, b) {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

func TestParseFeatures(t *testing.T) {
	cases := []struct {
		in   string
		want []string
		bad  bool
	}{
		{"queue", []string{"queue"}, false},
		{"stop,queue,brief", []string{"queue", "brief", "stop"}, false}, // table order, not argument order
		{" brief , brief ", []string{"brief"}, false},
		{"queue,mentor", nil, true},
		{"", nil, true},
		{",", nil, true},
		{"Queue", nil, true},
	}
	for _, c := range cases {
		got, err := ParseFeatures(c.in)
		if (err != nil) != c.bad || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: %v, %v", c.in, got, err)
		}
	}
	if _, err := ParseFeatures("nope"); err == nil || !strings.Contains(err.Error(), "known: queue, brief, stop") {
		t.Errorf("an unknown feature must list the known ones: %v", err)
	}
}

func TestInstallAllFeaturesIsIdempotentAndComplete(t *testing.T) {
	entries := EntriesFor(testBin, allFeatures())
	b, changed, err := MergeEntries(nil, entries)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	want := []string{"PreToolUse", "SessionStart", "Stop", "SubagentStop", "TaskCompleted", "TeammateIdle", "UserPromptSubmit"}
	if got := events(t, b); !reflect.DeepEqual(got, want) {
		t.Fatalf("events %v, want %v\n%s", got, want, b)
	}
	for _, c := range []string{testBin + " hook claude-pretooluse", testBin + " hook claude-sessionstart", testBin + " hook claude-stop\"",
		testBin + " hook claude-stop SubagentStop", testBin + " hook claude-stop TaskCompleted", testBin + " hook claude-stop TeammateIdle", testBin + " hook claude-userpromptsubmit"} {
		if !strings.Contains(string(b), c) {
			t.Errorf("missing %s in\n%s", c, b)
		}
	}
	if !strings.Contains(string(b), `"matcher": "startup|resume|compact"`) || !strings.Contains(string(b), `"matcher": "Bash"`) {
		t.Errorf("matchers:\n%s", b)
	}
	b2, changed, err := MergeEntries(b, entries)
	if err != nil || changed || string(b2) != string(b) {
		t.Fatalf("a second install must change nothing: changed=%v err=%v", changed, err)
	}
	un, changed, err := MergeUninstall(b)
	if err != nil || !changed || strings.Contains(string(un), "hooks") {
		t.Fatalf("uninstall must remove every heavyq hook and the empty containers: %v %v\n%s", changed, err, un)
	}
}

// Features are the exact set: installing a smaller one removes the hooks of the features left out, and foreign
// hooks on the same events stay.
func TestInstallIsDeclarativeAndKeepsForeignHooks(t *testing.T) {
	orig := []byte(`{"model":"opus","hooks":{
  "Stop":[{"hooks":[{"type":"command","command":"notify.sh"}]}],
  "SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":"load-context.sh"}]}],
  "Notification":{"odd":"shape that is none of our business"}}}`)
	full, _, err := MergeEntries(orig, EntriesFor(testBin, allFeatures()))
	if err != nil {
		t.Fatal(err)
	}
	h := hooksOf(t, full)
	if len(h["Stop"]) != 2 || len(h["SessionStart"]) != 2 {
		t.Fatalf("foreign Stop and SessionStart hooks must stay next to ours: %v", h)
	}
	queueOnly, changed, err := MergeEntries(full, EntriesFor(testBin, DefaultFeatures))
	if err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	h = hooksOf(t, queueOnly)
	if len(h["Stop"]) != 1 || len(h["SessionStart"]) != 1 || len(h["SubagentStop"]) != 0 || len(h["PreToolUse"]) != 1 {
		t.Fatalf("narrowing the features must remove the others' hooks: %v", h)
	}
	for _, keep := range []string{"notify.sh", "load-context.sh", `"model": "opus"`, "shape that is none of our business"} {
		if !strings.Contains(string(queueOnly), keep) {
			t.Errorf("lost %q:\n%s", keep, queueOnly)
		}
	}
	un, _, err := MergeUninstall(full)
	if err != nil {
		t.Fatal(err)
	}
	var a, z any
	_ = json.Unmarshal(orig, &a)
	_ = json.Unmarshal(un, &z)
	if !reflect.DeepEqual(a, z) {
		t.Fatalf("uninstall must restore the original:\n%s", un)
	}
}

func TestInstallMovedBinaryReplacesEveryEntry(t *testing.T) {
	b, _, _ := MergeEntries(nil, EntriesFor("/old/heavyq", allFeatures()))
	b2, changed, err := MergeEntries(b, EntriesFor("/new/heavyq", allFeatures()))
	if err != nil || !changed || strings.Contains(string(b2), "/old/heavyq") || strings.Count(string(b2), "/new/heavyq") != 6 {
		t.Fatalf("%v %v\n%s", changed, err, b2)
	}
}

func TestMergeRefusesWrongShapesOnEventsWeManage(t *testing.T) {
	for _, bad := range []string{`{"hooks":{"Stop":{}}}`, `{"hooks":{"SessionStart":"x"}}`, `{"hooks":{"TeammateIdle":3}}`, `{"hooks":[]}`} {
		if _, _, err := MergeEntries([]byte(bad), EntriesFor(testBin, allFeatures())); err == nil {
			t.Errorf("install accepted %s", bad)
		}
		if _, _, err := MergeUninstall([]byte(bad)); err == nil {
			t.Errorf("uninstall accepted %s", bad)
		}
	}
}

func TestOwnedSubcommand(t *testing.T) {
	yes := map[string]string{
		"heavyq hook claude-pretooluse":                  "claude-pretooluse",
		"'/a b/heavyq' hook claude-sessionstart":         "claude-sessionstart",
		"/usr/bin/heavyq hook claude-stop":               "claude-stop",
		"heavyq hook claude-stop SubagentStop":           "claude-stop",
		"  heavyq   hook   claude-stop   TeammateIdle  ": "claude-stop",
	}
	for cmd, want := range yes {
		if got, ok := ownedSubcommand(cmd); !ok || got != want {
			t.Errorf("%q: %q %v", cmd, got, ok)
		}
	}
	for _, cmd := range []string{"", "guard.sh", "heavyq hook other", "heavyq hook claude-stop a b",
		pluginCommand("claude-stop SubagentStop"), pluginCommand("claude-pretooluse"), "heavyq hook install claude"} {
		if _, ok := ownedSubcommand(cmd); ok {
			t.Errorf("%q must not be ours", cmd)
		}
	}
}

// A heavyq hook the installer does not manage (the mentor's PostToolUse hook, written by hand) survives install,
// narrowing and uninstall untouched.
func TestInstallLeavesHandWrittenHeavyqHooksAlone(t *testing.T) {
	orig := []byte(`{"hooks":{"PostToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"heavyq hook claude-posttooluse"}]}]}}`)
	full, _, err := MergeEntries(orig, EntriesFor(testBin, allFeatures()))
	if err != nil || !strings.Contains(string(full), "claude-posttooluse") {
		t.Fatalf("install must keep it: %v\n%s", err, full)
	}
	un, _, err := MergeUninstall(full)
	var a, z any
	_ = json.Unmarshal(orig, &a)
	_ = json.Unmarshal(un, &z)
	if err != nil || !reflect.DeepEqual(a, z) {
		t.Fatalf("uninstall must leave it: %v\n%s", err, un)
	}
}

package notify

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeBin puts an executable `name` that records its arguments (one per line)
// in a temp dir at the front of PATH: a real process stands in for the desktop
// notifier, so the exact argv is observable.
func fakeBin(t *testing.T, name string) (record string) {
	t.Helper()
	dir := t.TempDir()
	record = filepath.Join(dir, "argv.txt")
	// Written to a temp name and renamed, so a reader that sees the file sees all of it (a plain redirect creates it
	// empty first and fills it argument by argument).
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > '" + record + ".tmp'\nmv '" + record + ".tmp' '" + record + "'\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return record
}

func waitFile(t *testing.T, p string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(p); err == nil && len(b) > 0 {
			return string(b)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s never written", p)
	return ""
}

func envOf(kv map[string]string) Env { return func(k string) string { return kv[k] } }

func TestNotifySendReceivesTextAsArguments(t *testing.T) {
	rec := fakeBin(t, "notify-send")
	chans := DefaultChannels(envOf(map[string]string{"DISPLAY": ":0"}), "linux")
	if len(chans) == 0 || chans[0].Name() != "notify-send" {
		t.Fatalf("channels: %v", chans)
	}
	msg := Message{Title: "heavyq", Body: "needs you: $(touch /tmp/pwned) `x`\x1b]0;forged\x07"}
	if name, err := Deliver(chans, msg); err != nil || name != "notify-send" {
		t.Fatalf("deliver: %q %v", name, err)
	}
	got := strings.Split(strings.TrimSpace(waitFile(t, rec)), "\n")
	want := []string{"--app-name=heavyq", "--", "heavyq", "needs you: $(touch /tmp/pwned) `x`]0;forged"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("argv %q, want %q", got, want)
	}
}

func TestOsascriptTextIsNeverPartOfTheScriptSource(t *testing.T) {
	rec := fakeBin(t, "osascript")
	chans := DefaultChannels(envOf(nil), "darwin")
	if len(chans) == 0 || chans[0].Name() != "osascript" {
		t.Fatalf("channels: %v", chans)
	}
	if _, err := Deliver(chans, Message{Title: "heavyq", Body: `say "hi" & quit`}); err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(waitFile(t, rec)), "\n")
	if len(got) != 8 || got[6] != `say "hi" & quit` || got[7] != "heavyq" || strings.Contains(strings.Join(got[:6], ""), "say") {
		t.Errorf("argv %q: the text must be an argument, not script source", got)
	}
}

func TestChannelSelection(t *testing.T) {
	fakeBin(t, "notify-send")
	names := func(env map[string]string, goos string) string {
		var n []string
		for _, c := range DefaultChannels(envOf(env), goos) {
			n = append(n, c.Name())
		}
		return strings.Join(n, ",")
	}
	for _, tc := range []struct {
		name string
		env  map[string]string
		goos string
		want string
	}{
		{"desktop session: desktop first, terminal behind it", map[string]string{"DISPLAY": ":0"}, "linux", "notify-send,terminal"},
		{"no desktop session: terminal only", nil, "linux", "terminal"},
		{"over ssh the desktop is not yours", map[string]string{"DISPLAY": ":0", "SSH_CONNECTION": "1 2 3 4"}, "linux", "terminal"},
		{"off", map[string]string{"HEAVYQ_NOTIFY": "off", "DISPLAY": ":0"}, "linux", ""},
		{"terminal only", map[string]string{"HEAVYQ_NOTIFY": "terminal", "DISPLAY": ":0"}, "linux", "terminal"},
		{"desktop only", map[string]string{"HEAVYQ_NOTIFY": "desktop", "DISPLAY": ":0"}, "linux", "notify-send"},
		{"another OS has no desktop notifier here", nil, "plan9", "terminal"},
	} {
		if got := names(tc.env, tc.goos); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestTerminalSequences(t *testing.T) {
	m := Message{Title: "heavyq", Body: "a\nb\x1b[31m"}
	for _, tc := range []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"TERM_PROGRAM": "iTerm.app"}, "\x1b]9;heavyq: a | b[31m\x07"},
		{map[string]string{"TERM_PROGRAM": "WezTerm"}, "\x1b]9;heavyq: a | b[31m\x07"},
		{map[string]string{"TERM": "foot"}, "\x1b]777;notify;heavyq;a | b[31m\x07"},
		{map[string]string{"TERM_PROGRAM": "iTerm.app", "TMUX": "/tmp/x"}, "\a"}, // no passthrough assumed
		{map[string]string{"TERM": "xterm-256color"}, "\a"},
	} {
		var b bytes.Buffer
		if err := (TerminalChannel{Style: terminalStyle(envOf(tc.env)), Out: &b}).Send(m); err != nil {
			t.Fatal(err)
		}
		if b.String() != tc.want {
			t.Errorf("%v: %q, want %q", tc.env, b.String(), tc.want)
		}
	}
}

func TestTerminalWithoutATTYFailsSoTheNextChannelRuns(t *testing.T) {
	missing := TerminalChannel{Path: filepath.Join(t.TempDir(), "no-such-tty"), Style: styleBell}
	rec := &recorder{}
	name, err := Deliver([]Channel{missing, rec}, Message{Title: "t", Body: "b"})
	if err != nil || name != "rec" || rec.n.Load() != 1 {
		t.Errorf("fallback: %q %v", name, err)
	}
	if _, err := Deliver([]Channel{missing}, Message{}); err == nil || errors.Is(err, ErrNoChannel) {
		t.Errorf("a failing channel must report its error, got %v", err)
	}
}

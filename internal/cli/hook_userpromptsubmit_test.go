package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/hook"
	"github.com/LysCongThanh/heavyq/internal/stance"
)

// promptRig is a real git repository with a Vietnamese user stance and the
// real stance engine behind handlePrompt, with the clock fixed so that the
// output is the same on every run.
type promptRig struct {
	repo string
	env  stance.Env
}

func newPromptRig(t *testing.T) *promptRig {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HEAVYQ_HISTORY", filepath.Join(root, "state", "history.jsonl")) // hook.log goes here, not to the real state dir
	t.Setenv("HEAVYQ_LANE", "wt-auth")
	t.Setenv(stance.EnvStances, "")
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	userDir := filepath.Join(root, "user", "stances")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	viet := "+++\ndescription = \"Tư duy đột phá\"\nnext = \"viết đáp án từ đầu trước khi đọc thiết kế hiện tại\"\n[enter]\nkeywords = [\"đột phá\", \"tránh đại trà\"]\n+++\n" +
		"<!-- core -->\n- Hãy lùi lại và nghĩ từ gốc vấn đề, không chắp vá.\n- Liệt kê ít nhất 3 phương án, có một phương án triệt để.\n<!-- /core -->\n"
	if err := os.WriteFile(filepath.Join(userDir, "dot-pha.md"), []byte(viet), 0o644); err != nil {
		t.Fatal(err)
	}
	env := stance.Env{
		Dirs:   stance.Dirs{Repo: filepath.Join(repo, ".heavyq", "stances"), User: userDir},
		Trust:  stance.Trust{Path: filepath.Join(root, "state", "stance-trust.json")},
		Ledger: filepath.Join(root, "state", "history.jsonl"),
		Now:    func() time.Time { return time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC) },
	}
	return &promptRig{repo: repo, env: env}
}

func (p *promptRig) handle(t *testing.T, event, prompt string) hookReply {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"session_id": "sess-1", "cwd": p.repo, "hook_event_name": event, "prompt": prompt})
	in, err := hook.ParseInput(b)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := handlePrompt(p.env, in)
	if err != nil {
		t.Fatal(err)
	}
	return reply
}

func TestHandlePromptGoldenVietnameseKeyword(t *testing.T) {
	p := newPromptRig(t)
	reply := p.handle(t, "UserPromptSubmit", "Lỗi này vẫn chưa hết, tôi cần một hướng ĐỘT PHÁ.")
	if reply.Stdout == nil {
		t.Fatal("a Vietnamese keyword did not enter the stance")
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, reply.Stdout, "", "  "); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "userpromptsubmit_vietnamese.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(want)) != buf.String() {
		t.Errorf("golden mismatch\n got  %s\n want %s", buf.String(), want)
	}
	// Announced once: the same keyword again adds nothing.
	if again := p.handle(t, "UserPromptSubmit", "đột phá"); again.Stdout != nil {
		t.Errorf("second keyword: %s", again.Stdout)
	}
}

func TestHandlePromptSaysNothingByDefault(t *testing.T) {
	p := newPromptRig(t)
	for name, c := range map[string]struct {
		event, prompt string
		injects       bool
	}{
		"ordinary prompt": {"UserPromptSubmit", "please fix the login test", false},
		"empty prompt":    {"UserPromptSubmit", "", false},
		"other event":     {"PreToolUse", "đột phá", false},
		"no event name":   {"", "tránh đại trà", true}, // older payloads carry none
	} {
		if got := p.handle(t, c.event, c.prompt).Stdout != nil; got != c.injects {
			t.Errorf("%s: injected = %v, want %v", name, got, c.injects)
		}
	}
	t.Setenv(stance.EnvStances, "off")
	if p.handle(t, "UserPromptSubmit", "đột phá").Stdout != nil {
		t.Error("HEAVYQ_STANCES=off still injected")
	}
}

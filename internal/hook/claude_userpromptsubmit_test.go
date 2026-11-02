package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/stance"
)

// promptRig is a real git repository with a Vietnamese user stance, the real
// stance engine behind DecidePrompt, and the clock fixed so that the output
// is the same on every run.
type promptRig struct {
	repo string
	opts PromptOptions
}

func newPromptRig(t *testing.T) *promptRig {
	t.Helper()
	root := t.TempDir()
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
	return &promptRig{repo: repo, opts: PromptOptions{Enter: func(cwd, session, prompt string) (string, error) {
		e, err := env.OnPrompt(cwd, "wt-auth", session, prompt)
		if err != nil || e == nil {
			return "", err
		}
		return e.Context, nil
	}}}
}

func (p *promptRig) input(event, prompt string) []byte {
	b, _ := json.Marshal(map[string]string{"session_id": "sess-1", "cwd": p.repo, "hook_event_name": event, "prompt": prompt})
	return b
}

func TestDecidePromptGoldenVietnameseKeyword(t *testing.T) {
	p := newPromptRig(t)
	out, why, err := DecidePrompt(p.input("UserPromptSubmit", "Lỗi này vẫn chưa hết, tôi cần một hướng ĐỘT PHÁ."), p.opts)
	if err != nil || out == nil || why != "stance injected" {
		t.Fatalf("out=%s why=%q err=%v", out, why, err)
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, out, "", "  "); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "userpromptsubmit_vietnamese.golden.json")
	if *update {
		if err := os.WriteFile(path, append(buf.Bytes(), '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/hook -update)", err)
	}
	if strings.TrimSpace(string(want)) != buf.String() {
		t.Errorf("golden mismatch\n got  %s\n want %s", buf.String(), want)
	}
	// Announced once: the same keyword again adds nothing.
	if out, why, err := DecidePrompt(p.input("UserPromptSubmit", "đột phá"), p.opts); out != nil || err != nil || why != "no stance" {
		t.Errorf("second keyword: out=%s why=%q err=%v", out, why, err)
	}
}

func TestDecidePromptSaysNothingByDefault(t *testing.T) {
	p := newPromptRig(t)
	for name, c := range map[string]struct {
		in   []byte
		want string
	}{
		"ordinary prompt": {p.input("UserPromptSubmit", "please fix the login test"), "no stance"},
		"empty prompt":    {p.input("UserPromptSubmit", ""), "no prompt"},
		"other event":     {p.input("PreToolUse", "đột phá"), "event PreToolUse"},
		"no event name":   {p.input("", "tránh đại trà"), "stance injected"}, // older payloads carry none
	} {
		out, why, err := DecidePrompt(c.in, p.opts)
		if err != nil || (c.want == "stance injected") != (out != nil) || why != c.want {
			t.Errorf("%s: out=%s why=%q err=%v", name, out, why, err)
		}
	}
	if _, _, err := DecidePrompt([]byte("{not json"), p.opts); err == nil {
		t.Error("input that is not JSON must be an error (the caller fails open)")
	}
	if out, _, err := DecidePrompt(p.input("UserPromptSubmit", "đột phá"), PromptOptions{}); out != nil || err != nil {
		t.Errorf("without an engine: out=%s err=%v", out, err)
	}
}

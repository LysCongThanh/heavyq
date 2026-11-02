package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const e2eDecisionRecord = `# Decision record

## Clean-slate answer
Drop the retry loop: fail fast and report.

## Options
| Option | Value |
|---|---|
| Status quo: keep retrying | none |
| Incremental: cap the retries | fewer hangs |
| Radical: fail fast, no retries | no hangs |

## Constraints
- The API may time out: REAL.
- Retries were added "just in case": INERTIA.

## Choice and migration
Fail fast; roll out behind a flag.
`

// promptHook runs `heavyq hook claude-userpromptsubmit` with the given prompt.
func (h *stanceRig) promptHook(t *testing.T, prompt string, extraEnv ...string) (rc int, stdout string) {
	t.Helper()
	in, _ := json.Marshal(map[string]string{"session_id": "sess-1", "cwd": h.cwd, "hook_event_name": "UserPromptSubmit", "prompt": prompt})
	cmd := exec.Command(binPath, "hook", "claude-userpromptsubmit")
	cmd.Dir, cmd.Env = h.cwd, append(append([]string{}, h.env...), extraEnv...)
	cmd.Stdin = bytes.NewReader(in)
	var o bytes.Buffer
	cmd.Stdout = &o
	return processExit(cmd.Run()), o.String()
}

// The whole life of a stance: enter, status, refused exit, decision record, exit, stats.
func TestE2EStanceEnterStatusExitAfterDecisionRecord(t *testing.T) {
	h := newStanceRig(t)
	rc, out, errOut := h.split("stance", "enter", "breakthrough", "--why", "login keeps failing")
	if rc != 0 || errOut != "" || !strings.Contains(out, "[heavyq · stance breakthrough] why now: login keeps failing") ||
		!strings.Contains(out, "full: heavyq stance show breakthrough") {
		t.Fatalf("enter rc=%d stderr=%q\n%s", rc, errOut, out)
	}
	rc, out, _ = h.split("stance", "status")
	if rc != 0 || !strings.Contains(out, "stance breakthrough since") || !strings.Contains(out, "why: login keeps failing [manual]") ||
		!strings.Contains(out, "(missing)") || !strings.Contains(out, "evidence: same failure on 0 trees") {
		t.Fatalf("status rc=%d\n%s", rc, out)
	}
	var js struct {
		Active *struct{ Stance string }
		Record struct {
			Path   string
			Exists bool
		}
	}
	if _, out, _ = h.split("stance", "status", "--json"); json.Unmarshal([]byte(out), &js) != nil || js.Active == nil ||
		js.Active.Stance != "breakthrough" || js.Record.Exists || !strings.HasSuffix(js.Record.Path, filepath.Join(".heavyq", "decisions", "repo.md")) {
		t.Fatalf("status --json: %s", out)
	}

	// The exit is refused until the decision record is there and complete.
	rc, _, errOut = h.split("stance", "exit")
	if rc != 1 || !strings.Contains(errOut, "cannot leave stance breakthrough") || !strings.Contains(errOut, "no decision record yet") {
		t.Fatalf("exit without a record rc=%d stderr=%q", rc, errOut)
	}
	record := filepath.Join(h.repo, ".heavyq", "decisions")
	h.put(t, record, "repo.md", "# Decision record\n\n## Clean-slate answer\nsomething\n")
	if rc, _, errOut = h.split("stance", "exit"); rc != 1 || !strings.Contains(errOut, "missing section: options") {
		t.Fatalf("exit with an incomplete record rc=%d stderr=%q", rc, errOut)
	}
	h.put(t, record, "repo.md", e2eDecisionRecord)
	rc, out, errOut = h.split("stance", "exit")
	if rc != 0 || errOut != "" || !strings.Contains(out, "left stance breakthrough (decision record)") {
		t.Fatalf("exit with the record rc=%d stderr=%q\n%s", rc, errOut, out)
	}
	if _, out, _ = h.split("stance", "status"); !strings.Contains(out, "lane repo: no stance") {
		t.Fatalf("status after exit:\n%s", out)
	}
	rc, out, _ = h.split("stance", "stats")
	if rc != 0 || !strings.Contains(out, "breakthrough") || !strings.Contains(out, "manual 1") || !strings.Contains(out, "decision_record 1") {
		t.Fatalf("stats rc=%d\n%s", rc, out)
	}
	// Writing the record is bookkeeping: the work tree is as clean as before for git (the .heavyq dir is untracked only).
	if rc, _, errOut = h.split("stance", "exit"); rc != 1 || !strings.Contains(errOut, "is in no stance") {
		t.Errorf("exit while in no stance rc=%d stderr=%q", rc, errOut)
	}
}

func TestE2EStanceOffMutesUntilEnter(t *testing.T) {
	h := newStanceRig(t)
	h.split("stance", "enter", "calm")
	if rc, out, _ := h.split("stance", "off"); rc != 0 || !strings.Contains(out, "stances are off for lane repo") {
		t.Fatalf("off rc=%d %s", rc, out)
	}
	if _, out, _ := h.split("stance", "status"); !strings.Contains(out, "stances off") {
		t.Fatalf("status:\n%s", out)
	}
	if rc, out := h.promptHook(t, "I need a breakthrough"); rc != 0 || out != "" {
		t.Errorf("a muted lane was injected: rc=%d %s", rc, out)
	}
	if rc, _, _ := h.split("stance", "enter", "calm"); rc != 0 {
		t.Fatal("enter after off")
	}
	if _, out, _ := h.split("stance", "status"); !strings.Contains(out, "stance calm since") {
		t.Fatalf("status after enter:\n%s", out)
	}
}

func TestE2EPromptHookKeywordVietnameseAndSwitches(t *testing.T) {
	h := newStanceRig(t)
	h.put(t, h.userDir, "dot-pha.md", "+++\ndescription = \"Tư duy đột phá\"\n[enter]\nkeywords = [\"đột phá\"]\n+++\n<!-- core -->\n- Hãy nghĩ từ gốc vấn đề.\n<!-- /core -->\n")

	if rc, out := h.promptHook(t, "fix the failing test"); rc != 0 || out != "" {
		t.Fatalf("ordinary prompt rc=%d out=%q", rc, out)
	}
	rc, out := h.promptHook(t, "Cần hướng đột phá cho việc này")
	var got struct {
		Out struct {
			Event   string `json:"hookEventName"`
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if rc != 0 || json.Unmarshal([]byte(out), &got) != nil || got.Out.Event != "UserPromptSubmit" ||
		!strings.Contains(got.Out.Context, "[heavyq · stance dot-pha] why now:") || !strings.Contains(got.Out.Context, "Hãy nghĩ từ gốc vấn đề.") {
		t.Fatalf("keyword prompt rc=%d out=%q", rc, out)
	}
	if rc, out := h.promptHook(t, "đột phá"); rc != 0 || out != "" {
		t.Errorf("announced twice: rc=%d out=%q", rc, out)
	}
	if _, out, _ := h.split("stance", "status"); !strings.Contains(out, "stance dot-pha since") || !strings.Contains(out, "[keyword]") {
		t.Errorf("status after the hook:\n%s", out)
	}
	// HEAVYQ_STANCES=off and a broken engine both leave the prompt alone and exit 0.
	h.split("stance", "exit")
	if rc, out := h.promptHook(t, "đột phá", "HEAVYQ_STANCES=off"); rc != 0 || out != "" {
		t.Errorf("HEAVYQ_STANCES=off: rc=%d out=%q", rc, out)
	}
	h.put(t, h.userDir, "bad.md", "+++\nbogus = 1\n+++\nx\n")
	if rc, out := h.promptHook(t, "đột phá"); rc != 0 || out != "" {
		t.Errorf("an invalid stance file must fail open: rc=%d out=%q", rc, out)
	}
}

func TestE2ETrustGatesRepoStances(t *testing.T) {
	h := newStanceRig(t)
	stances := filepath.Join(h.repo, ".heavyq", "stances")
	file := h.put(t, stances, "shipit.md", "+++\ndescription = \"from the repo\"\n[enter]\nkeywords = [\"ship-it\"]\n+++\n<!-- core -->\n- Repo text.\n<!-- /core -->\n")

	// Listed, flagged, never injected.
	if rc, out, _ := h.split("stance", "ls"); rc != 0 || !strings.Contains(out, "shipit") || !strings.Contains(out, "(untrusted: run heavyq trust)") {
		t.Fatalf("ls rc=%d\n%s", rc, out)
	}
	if rc, out := h.promptHook(t, "ship-it"); rc != 0 || out != "" {
		t.Fatalf("untrusted stance injected: rc=%d out=%q", rc, out)
	}
	if rc, _, errOut := h.split("stance", "enter", "shipit"); rc != 1 || !strings.Contains(errOut, "heavyq trust") {
		t.Fatalf("manual enter rc=%d stderr=%q", rc, errOut)
	}
	// An agent cannot grant trust for itself.
	cmd := exec.Command(binPath, "trust")
	cmd.Dir, cmd.Env = h.cwd, append(append([]string{}, h.env...), "CLAUDECODE=1")
	var eb bytes.Buffer
	cmd.Stderr = &eb
	if processExit(cmd.Run()) != 1 || !strings.Contains(eb.String(), "trust is for a person") {
		t.Fatalf("trust under an agent: %q", eb.String())
	}
	if _, out, _ := h.split("trust", "--list"); !strings.Contains(out, "no trusted repositories") {
		t.Fatalf("list before trusting:\n%s", out)
	}

	rc, out, errOut := h.split("trust")
	if rc != 0 || !strings.Contains(out, "trusting 1 stance file(s)") || !strings.Contains(out, "sha256:") {
		t.Fatalf("trust rc=%d stderr=%q\n%s", rc, errOut, out)
	}
	if rc, out := h.promptHook(t, "ship-it"); rc != 0 || !strings.Contains(out, "[heavyq · stance shipit]") || !strings.Contains(out, "Repo text.") {
		t.Fatalf("trusted stance not injected: rc=%d out=%q", rc, out)
	}
	if _, out, _ := h.split("trust", "--list"); !strings.Contains(out, "sha256:") {
		t.Errorf("list after trusting:\n%s", out)
	}

	// An edit revokes the trust: untrusted again.
	h.split("stance", "exit", "--force")
	if err := os.WriteFile(file, []byte(strings.Replace(mustRead(t, file), "Repo text.", "Repo text, edited.", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, out, _ := h.split("stance", "ls"); !strings.Contains(out, "(untrusted: run heavyq trust)") {
		t.Fatalf("ls after an edit:\n%s", out)
	}
	if rc, out := h.promptHook(t, "ship-it"); rc != 0 || out != "" {
		t.Fatalf("an edited stance was injected without a new trust: rc=%d out=%q", rc, out)
	}
	h.split("trust") // trusting the edited content again
	if rc, _, _ := h.split("trust", "--revoke"); rc != 0 {
		t.Fatal("revoke")
	}
	if _, out, _ := h.split("trust", "--list"); !strings.Contains(out, "no trusted repositories") {
		t.Errorf("list after revoking:\n%s", out)
	}
}

func TestE2EStanceLaneUsageErrors(t *testing.T) {
	h := newStanceRig(t)
	for _, args := range [][]string{{"stance", "enter"}, {"stance", "enter", "a", "b"}, {"stance", "off", "x"}, {"trust", "--list", "--revoke"}, {"trust", "extra"}} {
		if rc, _, errOut := h.split(args...); rc != ExitUsage || !strings.Contains(errOut, "usage:") {
			t.Errorf("%v: rc=%d stderr=%q", args, rc, errOut)
		}
	}
	if rc, _, errOut := h.split("stance", "enter", "nope"); rc != 1 || !strings.Contains(errOut, `no stance named "nope"`) {
		t.Errorf("unknown stance: rc=%d stderr=%q", rc, errOut)
	}
}

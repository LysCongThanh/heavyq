package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/recap"
	"github.com/LysCongThanh/heavyq/internal/tui/render"
)

// withStdin runs heavyq in dir with stdin, returning its exit code, stdout and stderr.
func (h *rig) withStdin(dir, stdin string, env []string, args ...string) (rc int, stdout, stderr string) {
	cmd := exec.Command(binPath, args...)
	cmd.Dir, cmd.Env = dir, append(append([]string{}, h.env...), env...)
	cmd.Stdin = strings.NewReader(stdin)
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	if err := cmd.Start(); err != nil {
		h.t.Fatal(err)
	}
	return processExit(cmd.Wait()), o.String(), e.String()
}

// flowStateFor is a materialised flow state (schema 1, written by hand here as #102's refresh would).
func flowStateFor(updated time.Time) string {
	return fmt.Sprintf(`{"schema":1,"repo":"x","updated":%q,"goal":"g","run":null,
"items":{"total":8,"proven":5,"running":2,"blocked":1,"claimed_unproven":0,"open":0},
"needs_you":[{"item":"ui-item","lane":"app","session":"s","reason":"blocked: needs a Stripe test key","since":%q}],
"constraint":{"state":"ci_wait","share":0.41,"significant":true},
"throughput":{"proven_per_hour":2.1,"p50_claim_to_proof_s":1800,"window_s":14400,"n":9},
"lanes":[{"lane":"app","state":"proving","item":"web-item","since":%q}],"session":null}`,
		updated.UTC().Format(time.RFC3339), updated.UTC().Format(time.RFC3339), updated.UTC().Format(time.RFC3339))
}

// A real repository with two proven items (they share the web gate), a blocked one, a hand-ticked one and a failure repeated twice:
// the recap names each bucket with the command that acts on it, and --json says the same.
func TestE2ERecapOfARealRepository(t *testing.T) {
	h := newRig(t, baseConfig)
	h.env = append(h.env, "HEAVYQ_NOTIFY=off")
	app, _ := itemRepo(t, h, itemAnchor, "", nil)
	for _, step := range [][]string{{"item", "claim", "web-item"}, {"gate", "run", "web"}, {"item", "done", "web-item"},
		{"item", "block", "plain-item", "needs: a Stripe test key"}} {
		if rc, out, errOut := h.inDir(app, nil, step...); rc != 0 {
			t.Fatalf("%v: rc=%d\n%s%s", step, rc, out, errOut)
		}
	}
	for i := 0; i < 2; i++ { // the same failing command, on the same code, twice
		if rc, _, _ := h.inDir(app, nil, "5", "--lane", "app", "--", "sh", "-c", "exit 3"); rc != 3 {
			t.Fatalf("failing run %d: rc=%d", i, rc)
		}
	}

	rc, out, errOut := h.inDir(app, nil, "recap", "--ascii")
	if rc != 0 {
		t.Fatalf("recap rc=%d\n%s%s", rc, out, errOut)
	}
	for _, want := range []string{"2/5 proven", "forecast unknown", "next: heavyq item claim plain-item", "merged into main (2)",
		"claimed, unproven (1)", "hand-ticked", "blocked (1)", "needs: a Stripe test key", "wasted (1)", "failed 2x", "heavyq why app"} {
		if !strings.Contains(out, want) {
			t.Errorf("recap lacks %q:\n%s", want, out)
		}
	}
	if strings.ContainsAny(out, "◆⚑▲✓⚠") || strings.Contains(out, "\x1b") {
		t.Errorf("--ascii must have no glyphs or escapes:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "  ") && !strings.Contains(line, "heavyq ") && !strings.Contains(line, "git show") {
			t.Errorf("a bucket line without the command that acts on it: %q", line)
		}
	}

	rc, out, errOut = h.inDir(app, nil, "recap", "--json", "--since", "1h")
	var r recap.Recap
	if err := json.Unmarshal([]byte(out), &r); rc != 0 || err != nil {
		t.Fatalf("recap --json rc=%d err=%v\n%s%s", rc, err, out, errOut)
	}
	if r.Schema != 1 || r.Progress.Total != 5 || r.Progress.Proven != 2 || r.MergeRef != "main" || len(r.Merged) != 2 ||
		len(r.Blocked) != 1 || r.Blocked[0].Action != "heavyq item claim plain-item" || len(r.Wasted) != 1 || r.Wasted[0].Count != 2 ||
		r.Best == nil || r.Best.Command != "heavyq item claim plain-item" || r.Forecast.Status != "unknown" {
		t.Errorf("recap --json: %s", out)
	}

	for _, bad := range [][]string{{"recap", "--since", "soon"}, {"recap", "--since", "-1h"}, {"recap", "--since", "1h", "--run", "x"}, {"recap", "extra"}} {
		if rc, _, _ := h.inDir(app, nil, bad...); rc != ExitUsage {
			t.Errorf("%v: rc=%d, want usage error", bad, rc)
		}
	}
	if rc, _, errOut := h.inDir(app, nil, "recap", "--run", "nope"); rc != 1 || !strings.Contains(errOut, "no run") {
		t.Errorf("an unknown run: rc=%d %q", rc, errOut)
	}
}

// With the flow state present the recap forecasts from its throughput, and the
// status line prints the segment from the same file without a word on stderr.
func TestE2EStatuslineAndRecapShareTheFlowState(t *testing.T) {
	h := newRig(t, baseConfig)
	h.env = append(h.env, "HEAVYQ_NOTIFY=off", "NO_COLOR=1", "LANG=en_US.UTF-8")
	app, _ := itemRepo(t, h, itemAnchor, "", nil)
	statePath := filepath.Join(app, ".git", "heavyq", "flow", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte(flowStateFor(time.Now())), 0o644); err != nil {
		t.Fatal(err)
	}
	in := fmt.Sprintf(`{"cwd":%q,"workspace":{"current_dir":%q},"session_id":"s-1","model":{"id":"m"},"version":"2.0"}`, app, app)

	rc, out, errOut := h.withStdin(app, in, nil, "statusline", "--segment")
	if rc != 0 || errOut != "" || !strings.HasPrefix(out, "◆ 5/8 proven  ") || !strings.Contains(out, "⚑ 1 needs you") ||
		!strings.Contains(out, "▲ CI wait") || !strings.Contains(out, "✓ on track · ~") || strings.Count(out, "\n") != 1 {
		t.Fatalf("segment rc=%d stderr=%q\n%q", rc, errOut, out)
	}
	if rc, out, _ := h.withStdin(app, in, nil, "statusline", "--ascii"); rc != 0 || !strings.HasPrefix(render.StripSGR(out), "# 5/8 proven  ! 1 needs you  ^ CI wait") {
		t.Errorf("ascii: rc=%d %q", rc, out)
	}
	rc, wrapped, _ := h.withStdin(app, in, nil, "statusline", "--wrap", "echo first; echo second")
	if rc != 0 || !strings.HasPrefix(wrapped, "first\nsecond │ ◆ 5/8 proven") {
		t.Errorf("wrap: rc=%d %q", rc, wrapped)
	}
	// The session record the status line leaves for the ledger to join on.
	if b := mustRead(t, filepath.Join(app, ".git", "heavyq", "sessions", "s-1.meta.json")); !strings.Contains(b, `"version":"2.0"`) {
		t.Errorf("session meta: %s", b)
	}

	// A repository that never used heavyq: nothing printed, nothing written.
	plain := gitRepoAt(t, h, "plain")
	plainIn := fmt.Sprintf(`{"cwd":%q}`, plain)
	if rc, out, errOut := h.withStdin(plain, plainIn, nil, "statusline"); rc != 0 || out != "" || errOut != "" {
		t.Errorf("no state: rc=%d %q %q", rc, out, errOut)
	}
	if rc, _, _ := h.withStdin(app, in, nil, "statusline", "extra"); rc != ExitUsage {
		t.Errorf("a stray argument: rc=%d", rc)
	}

	rc, rec, errOut := h.inDir(app, nil, "recap")
	if rc != 0 || !strings.Contains(rec, "CI wait") || !strings.Contains(rec, "needs you") || !strings.Contains(rec, "(medium confidence, 9 finishes)") {
		t.Errorf("recap with a flow state: rc=%d\n%s%s", rc, rec, errOut)
	}
}

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/preflight"
)

// These tests run the real binary on a temp repository: a clean anchor is GO and
// opens a run span, a broken one is NO-GO with a numbered list of fixes.

const preflightGoAnchor = "# Goal: ship login v2\n\n- [ ] api: the API `proof: echo ok`\n"

const preflightNoGoAnchor = "# Goal: ship login v2\n\n" +
	"- [ ] api: the API `proof: sh -c true`\n" +
	"- [ ] ui: the form `proof: true` `needs: design sign-off`\n" +
	"- [ ] docs: the guide\n" +
	"- [ ] ghost: a tool nobody installed `proof: heavyq-no-such-tool-xyz --run` `after: nope`\n"

// preflightRepo is a committed repository with the given anchor (and settings, when not empty).
func preflightRepo(t *testing.T, h *rig, anchor, settings string) string {
	t.Helper()
	files := map[string]string{}
	if settings != "" {
		files[".claude/settings.json"] = settings
	}
	app, _ := itemRepo(t, h, anchor, "", files)
	h.env = append(h.env, "CLAUDE_CONFIG_DIR="+filepath.Join(h.root, "claude-home")) // never the developer's own settings
	return app
}

func spanFiles(t *testing.T, repo string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(repo, ".git", "heavyq", "runs", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestE2EPreflightGoOpensARunSpan(t *testing.T) {
	h := newRig(t, baseConfig)
	app := preflightRepo(t, h, preflightGoAnchor, "")
	rc, out, errOut := h.inDir(app, nil, "preflight", "--hours", "2", "--json")
	if rc != 0 {
		t.Fatalf("rc=%d\n%s\n%s", rc, out, errOut)
	}
	var doc struct {
		Verdict  string `json:"verdict"`
		Goal     string
		Hours    float64
		Parallel int
		Items    []string
		Skipped  []string
		Findings []struct{ Severity, Check string }
		Span     *struct {
			ID          string
			Started     time.Time
			ExpectedEnd *time.Time `json:"expected_end"`
			Items       []string
			Parallel    int
			Host        string
		}
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if doc.Verdict != "GO" || doc.Goal != "ship login v2" || doc.Hours != 2 || doc.Parallel != 3 ||
		len(doc.Items) != 1 || doc.Items[0] != "api" {
		t.Errorf("report: %+v", doc)
	}
	for _, f := range doc.Findings {
		if f.Severity == "block" {
			t.Errorf("a GO has no blocking finding: %+v", f)
		}
	}
	if len(doc.Skipped) != 1 || !strings.HasPrefix(doc.Skipped[0], "permissions: no Claude Code settings found") {
		t.Errorf("no Claude Code, no permission check: %v", doc.Skipped)
	}
	if doc.Span == nil || doc.Span.ExpectedEnd == nil || doc.Span.Host == "" || doc.Span.Parallel != 3 ||
		len(doc.Span.Items) != 1 || doc.Span.ExpectedEnd.Sub(doc.Span.Started).Round(time.Minute) != 2*time.Hour {
		t.Fatalf("span: %+v", doc.Span)
	}
	if files := spanFiles(t, app); len(files) != 1 || filepath.Base(files[0]) != doc.Span.ID+".json" {
		t.Errorf("span file: %v", files)
	}

	// current, ls and end read the span back.
	rc, cur, _ := h.inDir(app, nil, "run", "current", "--json")
	if rc != 0 || !strings.Contains(cur, doc.Span.ID) {
		t.Errorf("run current: rc=%d %s", rc, cur)
	}
	if rc, ls, _ := h.inDir(app, nil, "run", "ls"); rc != 0 || !strings.Contains(ls, doc.Span.ID) || !strings.Contains(ls, " open ") {
		t.Errorf("run ls: rc=%d %s", rc, ls)
	}
	if rc, end, _ := h.inDir(app, nil, "run", "end"); rc != 0 || !strings.Contains(end, "run span "+doc.Span.ID+" ended") {
		t.Errorf("run end: rc=%d %s", rc, end)
	}
	if rc, none, _ := h.inDir(app, nil, "run", "current"); rc != 1 || !strings.Contains(none, "no open run span") {
		t.Errorf("run current after end: rc=%d %s", rc, none)
	}
	if rc, none, _ := h.inDir(app, nil, "run", "current", "--json"); rc != 1 || strings.TrimSpace(none) != "null" {
		t.Errorf("run current --json after end: rc=%d %q", rc, none)
	}
	if rc, _, errOut := h.inDir(app, nil, "run", "end"); rc != 1 || !strings.Contains(errOut, "no open run span") {
		t.Errorf("run end with none open: rc=%d %s", rc, errOut)
	}
	if rc, out, _ := h.inDir(app, nil, "run", "end", doc.Span.ID); rc != 0 || !strings.Contains(out, "already ended") {
		t.Errorf("run end twice: rc=%d %s", rc, out)
	}
}

func TestE2EPreflightNoGoListsFixesAndOpensNoSpan(t *testing.T) {
	h := newRig(t, baseConfig)
	app := preflightRepo(t, h, preflightNoGoAnchor, `{"permissions":{"allow":["Bash(go test:*)"]}}`)
	rc, out, errOut := h.inDir(app, nil, "preflight")
	if rc != 1 {
		t.Fatalf("NO-GO exits 1, rc=%d\n%s\n%s", rc, out, errOut)
	}
	for _, want := range []string{
		"preflight: NO-GO (",
		"goal: ship login v2",
		"[anchor] item docs has no proof",
		"[anchor] item ghost: its proof cannot run: `heavyq-no-such-tool-xyz` is not installed",
		"[dependencies] item ui needs a human: design sign-off -> answer it, then delete the `needs:` span at .heavyq/anchor.md:4",
		`item ghost waits on "nope", which is not an item -> edit .heavyq/anchor.md:6`,
		"[permissions] `sh -c true` may prompt: no allow rule covers it -> edit .claude/settings.local.json: add \"Bash(sh -c true)\" to permissions.allow",
		"[permissions] `heavyq item done --run` may prompt",
		"claude -p --permission-prompts none",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 1 && line[0] >= '1' && line[0] <= '9' && line[1] == '.' && !strings.Contains(line, " -> ") {
			t.Errorf("a numbered line must end with its fix: %q", line)
		}
	}
	if strings.Contains(out, "run span") || len(spanFiles(t, app)) != 0 {
		t.Errorf("a NO-GO opens no run span:\n%s", out)
	}

	// --start opens one anyway, and says NO-GO all the same.
	rc, out, _ = h.inDir(app, nil, "preflight", "--start", "--json", "--parallel", "1")
	var doc struct {
		Verdict  string
		Parallel int
		Items    []string
		Span     *struct{ ID string }
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || rc != 1 || doc.Verdict != "NO-GO" || doc.Span == nil || doc.Parallel != 1 {
		t.Fatalf("--start: rc=%d err=%v\n%s", rc, err, out)
	}
	if strings.Join(doc.Items, ",") != "api,docs,ghost" { // ui waits on a human, so it is not planned
		t.Errorf("planned items: %v", doc.Items)
	}
	if len(spanFiles(t, app)) != 1 {
		t.Errorf("span files: %v", spanFiles(t, app))
	}
}

// The JSON of a NO-GO is pinned: field names and order, finding texts and fixes. Free disk space and the
// span's id and clock are the only machine-dependent parts and are masked.
func TestE2EPreflightJSONGolden(t *testing.T) {
	h := newRig(t, baseConfig)
	app := preflightRepo(t, h, preflightNoGoAnchor, `{"permissions":{"allow":["Bash(go test:*)"]}}`)
	rc, out, errOut := h.inDir(app, nil, "preflight", "--json", "--start", "--hours", "8")
	if rc != 1 {
		t.Fatalf("rc=%d\n%s\n%s", rc, out, errOut)
	}
	var doc struct {
		preflight.Report
		Span any `json:"span"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	doc.Span = "<span>"
	var kept []preflight.Finding
	for _, f := range doc.Findings {
		if f.Fix != "heavyq cache clear" { // the disk finding depends on the machine
			kept = append(kept, f)
		}
	}
	doc.Findings = kept
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		t.Fatal(err)
	}
	got := buf.Bytes()
	golden := filepath.Join("testdata", "preflight_no_go.golden.json")
	if os.Getenv("HEAVYQ_UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("golden mismatch (HEAVYQ_UPDATE_GOLDEN=1 go test ./internal/cli -run PreflightJSONGolden)\n got:\n%s", got)
	}
}

func TestE2EPreflightUsageAndAgentMode(t *testing.T) {
	h := newRig(t, baseConfig)
	app := preflightRepo(t, h, preflightGoAnchor, "")
	for _, args := range [][]string{{"preflight", "--hours", "-1"}, {"preflight", "--parallel", "99"}, {"preflight", "extra"}} {
		if rc, _, errOut := h.inDir(app, nil, args...); rc != ExitUsage || !strings.Contains(errOut, "usage: heavyq") {
			t.Errorf("%v: rc=%d %s", args, rc, errOut)
		}
	}
	if rc, _, errOut := h.inDir(h.root, nil, "preflight"); rc != ExitUsage || !strings.Contains(errOut, "not inside a git work tree") {
		t.Errorf("outside a repository: rc=%d %s", rc, errOut)
	}
	if rc, _, errOut := h.inDir(app, nil, "run", "bogus"); rc != ExitUsage || !strings.Contains(errOut, "first argument must be a priority 1-9 or a command") {
		t.Errorf("heavyq run bogus: rc=%d %s", rc, errOut)
	}
	rc, out, _ := h.inDir(app, []string{"HEAVYQ_AGENT=1"}, "preflight")
	if rc != 0 || !strings.HasSuffix(strings.TrimSpace(out), "next: heavyq item claim --next") {
		t.Errorf("agent mode ends with one next: line: rc=%d\n%s", rc, out)
	}
	rc, out, _ = h.inDir(app, nil, "run", "ls")
	if rc != 0 || !strings.Contains(out, " open ") {
		t.Errorf("the GO above opened a span: rc=%d %s", rc, out)
	}
}

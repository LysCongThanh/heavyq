package preflight

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// sampleReport is a NO-GO with one finding of each kind, built from the same
// pure checks the command runs.
func sampleReport() Report {
	items := []ItemFact{
		item("api", nil),
		item("ui", func(i *ItemFact) { i.Proof, i.Est = "", Estimate{} }),
		item("docs", func(i *ItemFact) { i.Needs, i.NeedsInFile, i.State = "design sign-off", true, "blocked" }),
	}
	rs := []Results{
		{Name: NameAnchor, Found: CheckAnchor(AnchorInput{Goal: "ship login v2", AnchorPath: ".heavyq/anchor.md", Items: items, Parallel: 3, LeaseTTL: 30 * time.Minute})},
		{Name: NameDependencies, Found: CheckDependencies(items)},
		{Name: NameEnvironment, Found: CheckEnvironment(EnvInput{Parallel: 3, DiskKnown: true, DiskFreeBytes: 3 << 30, DiskPath: "/repo",
			Machines: []MachineFact{{Name: "local", Local: true, Reachable: true, HeavySlots: 2}}})},
		{Name: NamePermissions, Skipped: "no Claude Code settings found"},
		{Name: NameQuota, Found: CheckQuota(&Quota{FiveHourPct: pct(85)}, 8)},
	}
	return Build(Plan{Goal: "ship login v2", Hours: 8, Parallel: 3, Items: []string{"api"}}, rs)
}

func TestReportJSONGolden(t *testing.T) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(sampleReport()); err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "report_no_go.golden.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("JSON differs from %s (go test ./internal/preflight -update to accept):\n%s", golden, buf.Bytes())
	}
}

func TestVerdictAndOrder(t *testing.T) {
	r := sampleReport()
	if r.Verdict != NoGo || r.Blocks() != 2 {
		t.Fatalf("verdict %s with %d blocks: %+v", r.Verdict, r.Blocks(), r.Findings)
	}
	seenWarn := false
	for _, f := range r.Findings {
		if f.Severity == Warn {
			seenWarn = true
		} else if seenWarn {
			t.Errorf("a block comes after a warning: %+v", r.Findings)
		}
	}
	if len(r.Checked) != 4 || len(r.Skipped) != 1 || r.Skipped[0] != "permissions: no Claude Code settings found" {
		t.Errorf("checked %v skipped %v", r.Checked, r.Skipped)
	}
	warnOnly := Build(Plan{Goal: "g"}, []Results{{Name: NameQuota, Found: []Finding{{Severity: Warn, Text: "t", Fix: "f"}}}})
	if warnOnly.Verdict != Go {
		t.Errorf("a warning alone is still GO: %+v", warnOnly)
	}
	empty := Build(Plan{}, nil)
	if empty.Verdict != Go || empty.Items == nil || empty.Findings == nil {
		t.Errorf("empty report must marshal arrays, not null: %+v", empty)
	}
}

func TestRender(t *testing.T) {
	var buf bytes.Buffer
	sampleReport().Render(&buf)
	out := buf.String()
	for _, want := range []string{
		"preflight: NO-GO (2 to handle now, 3 to watch)\n",
		"goal: ship login v2\n",
		"plan: 1 items, 3 lanes, 8 h\n",
		"1. [anchor] item ui has no proof",
		"-> edit .heavyq/anchor.md:3: add `proof: <command>` to ui\n",
		"[dependencies] item docs needs a human: design sign-off -> answer it, then delete the `needs:` span",
		"(watch) [quota]",
		"skipped permissions: no Claude Code settings found\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if len(line) > 0 && line[0] >= '1' && line[0] <= '9' && !strings.Contains(line, " -> ") {
			t.Errorf("a finding line must end with its fix: %q", line)
		}
	}
}

func TestRenderIsBoundedAndOneLine(t *testing.T) {
	var fs []Finding
	for i := 0; i < 30; i++ {
		fs = append(fs, Finding{Check: NameAnchor, Severity: Block, Text: "line one\nline two \x1b[31m", Fix: "fix"})
	}
	r := Build(Plan{Goal: "g\nh"}, []Results{{Name: NameAnchor, Found: fs}})
	var buf bytes.Buffer
	r.Render(&buf)
	out := buf.String()
	if !strings.Contains(out, "... 10 more (heavyq preflight --json)") || strings.Contains(out, "\x1b") || strings.Contains(out, "g\nh") {
		t.Errorf("bounded, control characters dropped:\n%s", out)
	}
	if n := strings.Count(out, "\n"); n > 30 {
		t.Errorf("%d lines", n)
	}
}

func TestP50ByCmdHash(t *testing.T) {
	rec := func(h string, secs float64, mod func(*history.Record)) history.Record {
		r := history.Record{CmdHash: h, RunSecs: secs}
		if mod != nil {
			mod(&r)
		}
		return r
	}
	d := P50ByCmdHash([]history.Record{
		rec("a", 10, nil), rec("a", 30, nil), rec("a", 20, nil), // median 20
		rec("b", 4, nil), rec("b", 8, nil), // even count: mean of the middle two
		rec("c", 99, func(r *history.Record) { r.Exit = 1 }),
		rec("c", 99, func(r *history.Record) { r.Cache = "hit" }),
		rec("c", 99, func(r *history.Record) { r.Result = history.OutcomeCancelled }),
		rec("", 5, nil),
		rec("e", 12, nil),
	})
	if d["a"] != 20*time.Second || d["b"] != 6*time.Second || d["e"] != 12*time.Second {
		t.Errorf("%v", d)
	}
	if _, ok := d["c"]; ok || len(d) != 3 {
		t.Errorf("failed, replayed and cancelled runs have no duration: %v", d)
	}
	if e := d.Estimate([]string{"a", "b"}); !e.Known || e.P50 != 26*time.Second {
		t.Errorf("sum of medians: %+v", e)
	}
	if e := d.Estimate([]string{"a", "zzz"}); e.Known {
		t.Errorf("one unknown command makes the proof unknown: %+v", e)
	}
	if e := d.Estimate(nil); e.Known {
		t.Errorf("no command, no estimate: %+v", e)
	}
}

func TestLoadRules(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a := write("a.json", `{"permissions":{"allow":["Bash(go test:*)"]}}`)
	b := write("b.json", `{"permissions":{"allow":["Bash(ls:*)"]}}`)
	bad := write("bad.json", `{nope`)
	r, found, problems := LoadRules([]string{a, filepath.Join(dir, "missing.json"), bad, b})
	if len(r.Allow) != 2 || len(found) != 2 || len(problems) != 1 || !strings.Contains(problems[0], "bad.json") {
		t.Errorf("allow %v found %v problems %v", r.Allow, found, problems)
	}
	if got := SettingsPaths("/r", "/home/u/.claude"); len(got) != 3 || got[2] != "/home/u/.claude/settings.json" {
		t.Errorf("%v", got)
	}
	if got := SettingsPaths("/r", ""); len(got) != 2 {
		t.Errorf("%v", got)
	}
}

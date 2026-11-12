package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fleetFixture lays out a foreign queue the documented way in the rig: a TSV history, a tickets dir with one
// running and one waiting ticket (both owned by this test process, so they are live), and a machines dir.
func fleetFixture(t *testing.T, h *rig) string {
	t.Helper()
	fq := filepath.Join(h.root, "fq")
	if err := os.MkdirAll(filepath.Join(fq, "machines.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	var hist strings.Builder
	fmt.Fprintf(&hist, "%d\twatch\told-lane\t1\t0\t5\t5\t0\n", now-90000) // outside the 24h window
	for i := 0; i < 10; i++ {
		exit := []int{0, 0, 0, 1, 255, 0, 97, 0, 0, 137}[i]
		fmt.Fprintf(&hist, "%d\twatch\tapi\t1\t0\t%d\t900\t%d\n", now-3600+int64(i), 60*(i+1), exit)
	}
	fmt.Fprintf(&hist, "%d\tbuild-1\tweb\t3\t1\t30\t60\t0\n", now-60)
	ns := fmt.Sprintf("%020d", time.Now().Add(-40*time.Minute).UnixNano())
	pid := os.Getpid()
	files := map[string]string{
		"history.tsv":                           hist.String(),
		fmt.Sprintf("1-%s-%d-api.run", ns, pid): fmt.Sprintf("pid=%d lane=api machine=watch slot=s1 cmd=bash scripts/verify.sh", pid),
		fmt.Sprintf("2-%s-%d-web", ns, pid):     fmt.Sprintf("pid=%d lane=web cmd=npm test", pid),
		"machines.d/watch.env":                  "KIND=linux-x64\nSLOTS=\"s1 s2 s3\"\nLIGHT_SLOTS=\"s4\"\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(fq, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// "watch" is observed through `sh -c` (this machine); "dead" fails like an ssh that cannot connect (a hanging
	// machine is TestProbeOfAHangingMachineTimesOut: here the timeout is generous, for a loaded test machine).
	return baseConfig + `[[remote]]
name = "watch"
transport = ["sh", "-c"]
observe_only = true
[[remote]]
name = "dead"
transport = ["sh", "-c", "exit 255"]
git_url = "/nonexistent"
[[foreign]]
name = "bashq"
history = "` + filepath.Join(fq, "history.tsv") + `"
tickets = "` + fq + `"
infra_exits = [97]
machines_dir = "` + filepath.Join(fq, "machines.d") + `"
aliases = { "build-1" = "local" }
`
}

func TestE2EFleetObservesEveryQueue(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(fleetFixture(t, h))
	if rc, out := h.run("3", "--remote", "local", "--lane", "gate", "--", "true"); rc != 0 { // one heavyq run in the ledger
		t.Fatalf("rc=%d\n%s", rc, out)
	}

	start := time.Now()
	rc, out := h.run("fleet", "--json", "--timeout", "45s")
	if rc != 0 || time.Since(start) > 50*time.Second {
		t.Fatalf("rc=%d after %s\n%s", rc, time.Since(start), out)
	}
	var r struct {
		Schema   int `json:"schema"`
		Machines []struct {
			Name      string `json:"name"`
			Role      string `json:"role"`
			Reachable bool   `json:"reachable"`
			Cores     int    `json:"cores"`
			Cap       *struct {
				Workers int `json:"workers"`
			} `json:"worker_cap"`
		} `json:"machines"`
		Queues []struct {
			Name   string `json:"name"`
			Window struct {
				Runs     int `json:"runs"`
				RedInfra int `json:"red_infra"`
				RedCode  int `json:"red_code"`
			} `json:"window"`
			Running []struct{ Lane string } `json:"running"`
			Waiting []struct{ Lane string } `json:"waiting"`
		} `json:"queues"`
	}
	if err := json.Unmarshal([]byte(out[strings.Index(out, "{"):]), &r); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	roles := map[string]string{}
	for _, m := range r.Machines {
		roles[m.Name] = fmt.Sprintf("%s reachable=%v", m.Role, m.Reachable)
		if m.Name == "watch" && (m.Cores < 1 || m.Cap == nil || m.Cap.Workers < 1) {
			t.Errorf("watch is probed and gets a cap from its 3 foreign heavy slots: %+v", m)
		}
	}
	if roles["local"] != "local reachable=true" || roles["watch"] != "observe_only reachable=true" || roles["dead"] != "remote reachable=false" {
		t.Fatalf("machines: %v\n%s", roles, out)
	}
	byName := map[string]int{}
	for i, q := range r.Queues {
		byName[q.Name] = i
	}
	hq, bq := r.Queues[byName["heavyq"]], r.Queues[byName["bashq"]]
	if hq.Window.Runs != 1 || bq.Window.Runs != 11 || bq.Window.RedInfra != 3 || bq.Window.RedCode != 1 {
		t.Fatalf("windows: heavyq %+v bashq %+v", hq.Window, bq.Window)
	}
	if len(bq.Running) != 1 || bq.Running[0].Lane != "api" || len(bq.Waiting) != 1 || bq.Waiting[0].Lane != "web" {
		t.Fatalf("live: %+v %+v", bq.Running, bq.Waiting)
	}

	// --md writes the report atomically for other agents, and says nothing on stdout (cron-friendly).
	md := filepath.Join(h.root, "reports", "fleet.md")
	if err := os.MkdirAll(filepath.Dir(md), 0o755); err != nil {
		t.Fatal(err)
	}
	if rc, out := h.run("fleet", "--md", md, "--timeout", "45s"); rc != 0 || strings.TrimSpace(out) != "" {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	body := h.read("reports/fleet.md")
	for _, want := range []string{"# Fleet report", "Generated at:", "Window: 24h", "bashq", "watch", "dead"} {
		if !strings.Contains(body, want) {
			t.Errorf("markdown: no %q\n%s", want, body)
		}
	}
	if ents, _ := os.ReadDir(filepath.Dir(md)); len(ents) != 1 {
		t.Fatalf("temporary files left: %v", ents)
	}

	// Agent mode is a compact digest under the 4 KB budget.
	if rc, out := h.run("fleet", "--agent", "--timeout", "45s"); rc != 0 || len(out) >= 4096 || !strings.Contains(out, "bashq") {
		t.Fatalf("rc=%d (%d bytes)\n%s", rc, len(out), out)
	}
	// Unsafe input fails closed before anything is probed.
	if rc, _ := h.run("fleet", "--since", "-1h"); rc != ExitUsage {
		t.Fatalf("a negative window is a usage error, got %d", rc)
	}
}

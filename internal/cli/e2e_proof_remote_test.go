package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests use the fake remote of e2e_remote_status_test.go ("box", reached
// through `sh -c`, so no ssh is needed) to show where proofs run: `[proof]
// remote` in the config, the --remote flag over it, and what the run ledger
// says about each run.

// proofRun is the part of a history line these tests read.
type proofRun struct {
	Machine string `json:"machine"`
	Item    string `json:"item"`
	Gate    string `json:"gate"`
	Exit    int    `json:"exit"`
	RunID   string `json:"run_id"`
	Tree    string `json:"tree"`
	Cmd     string `json:"cmd"`
}

// proofRuns are the ledger lines of the rig, in order.
func (h *rig) proofRuns() []proofRun {
	h.t.Helper()
	var out []proofRun
	for _, line := range strings.Split(strings.TrimSpace(h.read("history.jsonl")), "\n") {
		if line == "" {
			continue
		}
		var r proofRun
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			h.t.Fatalf("history line %q: %v", line, err)
		}
		out = append(out, r)
	}
	return out
}

// machinesOf lists the machines that ran something tagged (item or gate) tag.
func (h *rig) machinesOf(tag string) []string {
	var ms []string
	for _, r := range h.proofRuns() {
		if r.Item == tag || r.Gate == tag {
			ms = append(ms, r.Machine)
		}
	}
	return ms
}

func (h *rig) wantMachines(tag string, want ...string) {
	h.t.Helper()
	if got := h.machinesOf(tag); strings.Join(got, ",") != strings.Join(want, ",") {
		h.t.Fatalf("runs tagged %q went to %v, want %v\nhistory:\n%s", tag, got, want, h.read("history.jsonl"))
	}
}

const proofRemoteAnchor = "# Goal: proofs run where heavy work goes\n\n" +
	"- [ ] cfg-cmd: a command proof `proof: echo run >> {ROOT}/count-a; echo ok`\n" +
	"- [ ] flag-local: forced to this machine `proof: echo run >> {ROOT}/count-b; echo ok`\n" +
	"- [ ] cfg-gate: a gate proof `proof: gate:web`\n" +
	"- [ ] flag-auto: the scheduler chooses `proof: echo run >> {ROOT}/count-c; echo ok`\n" +
	"- [ ] dirty-named: uncommitted code, named remote `proof: echo run >> {ROOT}/count-d; echo ok`\n" +
	"- [ ] dirty-auto: uncommitted code, auto `proof: echo run >> {ROOT}/count-e; echo ok`\n"

func (h *rig) lines(name string) int {
	b, _ := os.ReadFile(filepath.Join(h.root, name))
	return strings.Count(string(b), "\n")
}

func (h *rig) mustDone(dir, id string, extra ...string) (stdout, stderr string) {
	h.t.Helper()
	if rc, out, errOut := h.inDir(dir, nil, "item", "claim", id); rc != 0 {
		h.t.Fatalf("claim %s rc=%d\n%s%s", id, rc, out, errOut)
	}
	rc, out, errOut := h.inDir(dir, nil, append([]string{"item", "done", id, "--run"}, extra...)...)
	if rc != 0 || !strings.Contains(out, "done "+id+": proven") {
		h.t.Fatalf("item done %s %v rc=%d\n%s%s\nhistory:\n%s", id, extra, rc, out, errOut, h.read("history.jsonl"))
	}
	return out, errOut
}

// TestE2EItemDoneRunUsesProofRemote is the proof of anchor item proof-remote:
// `item done --run` runs a proof where [proof] remote says, --remote overrides
// it, and a run on the remote counts as proof for the same tree.
func TestE2EItemDoneRunUsesProofRemote(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, "[proof]\nremote = \"box\"\n"))
	app, _ := itemRepo(t, h, proofRemoteAnchor, "", nil)

	// [proof] remote = "box": a command proof and a gate proof both run there,
	// and the item is done: the remote's run is proof for this tree.
	h.mustDone(app, "cfg-cmd")
	h.wantMachines("cfg-cmd", "box")
	if n := h.lines("count-a"); n != 1 {
		t.Fatalf("the proof command ran %d times, want 1", n)
	}
	h.mustDone(app, "cfg-gate")
	h.wantMachines("web", "box")

	// --remote beats the config.
	h.mustDone(app, "flag-local", "--remote", "local")
	h.wantMachines("flag-local", "local")

	// --remote auto goes through the scheduler: local or box, whichever it prefers.
	h.mustDone(app, "flag-auto", "--remote", "auto")
	if ms := h.machinesOf("flag-auto"); len(ms) != 1 || (ms[0] != "local" && ms[0] != "box") {
		t.Fatalf("auto ran on %v", ms)
	}

	// Uncommitted code exists only here. A named remote would run the commit, not the edit: refused,
	// nothing runs, and the message says how to go on.
	if err := os.WriteFile(filepath.Join(app, "scratch.txt"), []byte("edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rc, out, errOut := h.inDir(app, nil, "item", "claim", "dirty-named"); rc != 0 {
		t.Fatalf("claim rc=%d\n%s%s", rc, out, errOut)
	}
	rc, _, errOut := h.inDir(app, nil, "item", "done", "dirty-named", "--run")
	if rc != 1 || !strings.Contains(errOut, "uncommitted changes") || !strings.Contains(errOut, "--remote local") {
		t.Fatalf("a dirty tree with a named remote must be refused: rc=%d\n%s", rc, errOut)
	}
	if n := h.lines("count-d"); n != 0 || len(h.machinesOf("dirty-named")) != 0 {
		t.Fatalf("a refused proof must not run: count-d=%d, runs %v", n, h.machinesOf("dirty-named"))
	}
	h.mustDone(app, "dirty-named", "--remote", "local")
	h.wantMachines("dirty-named", "local")

	// auto + uncommitted code: stays on this machine, and says so.
	_, errOut = h.mustDone(app, "dirty-auto", "--remote", "auto")
	if !strings.Contains(errOut, "uncommitted changes: the proof runs locally") {
		t.Fatalf("missing the fallback note:\n%s", errOut)
	}
	h.wantMachines("dirty-auto", "local")
}

// An unknown --remote is a usage error that names it; the item stays open.
func TestE2EItemDoneRunRefusesUnknownRemote(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	app, _ := itemRepo(t, h, proofRemoteAnchor, "", nil)
	if rc, out, errOut := h.inDir(app, nil, "item", "claim", "cfg-cmd"); rc != 0 {
		t.Fatalf("claim rc=%d\n%s%s", rc, out, errOut)
	}
	rc, _, errOut := h.inDir(app, nil, "item", "done", "cfg-cmd", "--run", "--remote", "ghost")
	if rc != ExitUsage || !strings.Contains(errOut, "ghost") || !strings.Contains(errOut, "--remote") {
		t.Fatalf("rc=%d\n%s", rc, errOut)
	}
	if rc, _, errOut := h.inDir(app, nil, "item", "done", "cfg-cmd", "--remote", "box"); rc != ExitUsage || !strings.Contains(errOut, "only applies with --run") {
		t.Fatalf("--remote without --run: rc=%d\n%s", rc, errOut)
	}
	if got := h.proofRuns(); len(got) != 0 || h.lines("count-a") != 0 {
		t.Fatalf("nothing may run: %+v", got)
	}
}

// A gate run honours [proof] remote from the user config, from heavyq.toml,
// and --remote over both.
func TestE2EGateRunUsesProofRemote(t *testing.T) {
	cases := []struct {
		name, user, repoToml string
		args                 []string
		want                 string
	}{
		{"user config", "[proof]\nremote = \"box\"\n", "", nil, "box"},
		{"heavyq.toml", "", "[proof]\nremote = \"box\"\n", nil, "box"},
		{"flag over config", "[proof]\nremote = \"box\"\n", "", []string{"--remote", "local"}, "local"},
		{"default is local", "", "", nil, "local"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newRig(t, "")
			h.setConfig(remoteConfig(h, tc.user))
			app, _ := itemRepo(t, h, "# Goal: x\n", tc.repoToml, nil)
			rc, out, errOut := h.inDir(app, nil, append([]string{"gate", "run", "web"}, tc.args...)...)
			if rc != 0 {
				t.Fatalf("gate run rc=%d\n%s%s", rc, out, errOut)
			}
			h.wantMachines("web", tc.want)
			if rc, out, errOut := h.inDir(app, nil, "gate", "status", "web"); rc != 0 {
				t.Fatalf("a run on %s must prove the gate: rc=%d\n%s%s", tc.want, rc, out, errOut)
			}
		})
	}
}

// A bad remote in [proof] fails where it is used and names where it was set.
func TestE2EGateRunRefusesUnknownProofRemote(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	app, _ := itemRepo(t, h, "# Goal: x\n", "[proof]\nremote = \"ghost\"\n", nil)
	rc, _, errOut := h.inDir(app, nil, "gate", "run", "web")
	if rc != ExitUsage || !strings.Contains(errOut, "ghost") || !strings.Contains(errOut, "heavyq.toml") {
		t.Fatalf("rc=%d\n%s", rc, errOut)
	}
	if got := h.proofRuns(); len(got) != 0 {
		t.Fatalf("nothing may run: %+v", got)
	}
}

// The run_id does not depend on the machine: the same command on the same code
// has the same identity run here or on a remote, which is why a proof run on a
// remote counts for the tree it ran on.
func TestE2ERunIDIsTheSameOnLocalAndRemote(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	app := gitRepo(t, h)
	for _, where := range []string{"local", "box"} {
		if rc, out := h.run("3", "--remote", where, "--", "sh", "-c", "echo same-command"); rc != 0 {
			t.Fatalf("%s rc=%d\n%s", where, rc, out)
		}
	}
	runs := h.proofRuns()
	if len(runs) != 2 || runs[0].Machine != "local" || runs[1].Machine != "box" {
		t.Fatalf("history: %+v", runs)
	}
	if runs[0].RunID == "" || runs[0].RunID != runs[1].RunID || runs[0].Tree == "" || runs[0].Tree != runs[1].Tree {
		t.Fatalf("run identity depends on the machine:\nlocal %+v\nbox   %+v\nrepo %s", runs[0], runs[1], app)
	}
}

package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests use the fake remotes of e2e_remote_status_test.go (reached
// through `sh -c`) to show an ordered --remote list: the run goes to the first
// machine of the list that has a free slot.

// twoRemotesConfig is remoteConfig with a second remote, "box2".
func twoRemotesConfig(h *rig, extra string) string {
	work := filepath.Join(h.root, "remote-work2")
	return remoteConfig(h, `[[remote]]
name = "box2"
transport = ["sh", "-c"]
work_dir = "`+work+`"
git_url = "`+filepath.Join(work, "git")+`"
heavy_slots = 1
`+extra)
}

// machineOfLane is the machine the last run of lane went to ("" if none).
func (h *rig) machineOfLane(lane string) string {
	h.t.Helper()
	m := ""
	for _, r := range h.history() {
		if r.Lane == lane {
			m = r.Machine
		}
	}
	return m
}

func TestE2ERemoteListTakesTheFirstMachineThatFits(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, "")) // local and box: one heavy slot each
	gitRepo(t, h)
	for _, c := range []struct{ list, lane, want string }{
		{"box,local", "box-first", "box"},
		{"local,box", "local-first", "local"},
	} {
		if rc, out := h.run("3", "--remote", c.list, "--lane", c.lane, "--", "true"); rc != 0 {
			t.Fatalf("--remote %s: rc=%d\n%s", c.list, rc, out)
		}
		if got := h.machineOfLane(c.lane); got != c.want {
			t.Fatalf("--remote %s ran on %q, want %q (the list order, not the score)", c.list, got, c.want)
		}
	}

	// box's only slot is busy: the next machine of the list takes the run.
	hold := h.start("3", "--remote", "box", "--lane", "hold", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	if rc, out := h.run("3", "--remote", "box,local", "--lane", "fallback", "--", "true"); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	h.touch("go")
	hold.wait(t, 20*time.Second)
	if got := h.machineOfLane("fallback"); got != "local" {
		t.Fatalf("with box busy the run went to %q, want local", got)
	}
}

func TestE2ERemoteListRefusesBadLists(t *testing.T) {
	h := newRig(t, "")
	h.setConfig(remoteConfig(h, ""))
	gitRepo(t, h)
	for list, want := range map[string]string{
		"auto,box":   "cannot be in a list",
		"box,ghost":  "ghost",
		"box,box":    "listed twice",
		"box, local": "no spaces",
		"box,":       "not \"local\" or a remote name",
	} {
		rc, out := h.run("3", "--remote", list, "--", "true")
		if rc != ExitUsage || !strings.Contains(out, want) {
			t.Fatalf("--remote %q: rc=%d, want %d and %q\n%s", list, rc, ExitUsage, want, out)
		}
	}
	if got := h.history(); len(got) != 0 {
		t.Fatalf("nothing may run: %+v", got)
	}
}

// A proof on uncommitted code: a list that names local falls back to it (like
// auto); a list of remotes only is refused (like a named remote).
func TestE2EGateRunWithAMachineListOnADirtyTree(t *testing.T) {
	cases := []struct {
		name, list string
		rc         int
		want       string // in stderr
		ran        []string
	}{
		{"names local", "box,local", 0, "box,local only places committed code", []string{"local"}},
		{"remotes only", "box,box2", 1, "on remote box,box2", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newRig(t, "")
			h.setConfig(twoRemotesConfig(h, "[proof]\nremote = \""+tc.list+"\"\n"))
			app, _ := itemRepo(t, h, "# Goal: x\n", "", nil)
			h.edit(app, "uncommitted\n")
			rc, out, errOut := h.inDir(app, nil, "gate", "run", "web")
			if rc != tc.rc || !strings.Contains(errOut, tc.want) {
				t.Fatalf("rc=%d (want %d), stderr must say %q\n%s%s", rc, tc.rc, tc.want, out, errOut)
			}
			h.wantMachines("web", tc.ran...)
		})
	}

	// Committed code goes to the first machine of the list.
	h := newRig(t, "")
	h.setConfig(twoRemotesConfig(h, "[proof]\nremote = \"box2,local,box\"\n"))
	app, _ := itemRepo(t, h, "# Goal: x\n", "", nil)
	if rc, out, errOut := h.inDir(app, nil, "gate", "run", "web"); rc != 0 {
		t.Fatalf("gate run rc=%d\n%s%s", rc, out, errOut)
	}
	h.wantMachines("web", "box2")
}

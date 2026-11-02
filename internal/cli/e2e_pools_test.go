package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/pools"
	"github.com/LysCongThanh/heavyq/internal/slots"
)

// Pools are leased with the slot, by the heavyq process of the run: real
// processes, real flocks, real children, as in the other e2e tests.

const poolConfig = `[queue]
poll = "50ms"
resource_gate = false
[local]
heavy_slots = 6
light_slots = 1
job_mb = 1
light_mb = 1
max_load_ratio = 64
[[pool]]
name = "pg"
values = ["app_wt1", "app_wt2"]
[[pool]]
name = "port"
values = "4100-4103"
[[pool]]
name = "browser"
size = 2
[[pool]]
name = "one"
size = 1
`

// poolLocked reports whether the lock file of a pool value is held right now.
func (h *rig) poolLocked(pool, value string) bool {
	return slots.Busy(filepath.Join(h.root, "q", "pools", pool, value+".lock"))
}

// Five runs need pg (two values): never more than two at once, never the same
// value twice at once, and every one of them gets to run.
func TestPoolNeverOverlapsAndEveryRunGetsItsValue(t *testing.T) {
	h := newRig(t, poolConfig)
	active := filepath.Join(h.root, "active")
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatal(err)
	}
	// Taking the value's directory fails when another run holds the same value.
	script := fmt.Sprintf(`mkdir %[1]s/$HEAVYQ_PG || echo clash >> %[2]s/clash
n=$(ls %[1]s | wc -l); echo $n >> %[2]s/counts
sleep 0.4
rmdir %[1]s/$HEAVYQ_PG`, active, h.root)
	var ps []*proc
	for i := range 5 {
		ps = append(ps, h.start("3", "--lane", "l"+strconv.Itoa(i), "--needs", "pg", "--", "sh", "-c", script))
	}
	for _, p := range ps {
		if rc := p.wait(t, 40*time.Second); rc != 0 {
			t.Fatalf("rc=%d\n%s", rc, p.out)
		}
	}
	if h.read("clash") != "" {
		t.Fatal("two runs held the same pg value at once")
	}
	peak := 0
	for _, f := range strings.Fields(h.read("counts")) {
		n, _ := strconv.Atoi(f)
		peak = max(peak, n)
	}
	if peak != 2 {
		t.Fatalf("up to %d runs held pg at once, want exactly 2 (counts %q)", peak, h.read("counts"))
	}
	if len(strings.Fields(h.read("counts"))) != 5 {
		t.Fatalf("not every run ran: %q", h.read("counts"))
	}
}

// A ticket that needs port:2 and pg while pg is taken waits holding no port,
// and gets everything, with its environment, once pg is free.
func TestPoolNeedsAreAtomicAndExported(t *testing.T) {
	h := newRig(t, poolConfig)
	holders := []*proc{
		h.start("3", "--lane", "pg1", "--needs", "pg", "--", "sh", "-c", h.gateFile("go1")),
		h.start("3", "--lane", "pg2", "--needs", "pg", "--", "sh", "-c", h.gateFile("go2")),
	}
	h.running(2)
	out := filepath.Join(h.root, "env.txt")
	want := h.start("3", "--lane", "wide", "--needs", "port:2,pg,browser", "--", "sh", "-c",
		fmt.Sprintf(`echo "$HEAVYQ_PORT_1 $HEAVYQ_PORT_2 $HEAVYQ_PG $HEAVYQ_BROWSER" > %s`, out))
	h.waiting(1)
	time.Sleep(300 * time.Millisecond) // several scheduling rounds
	for _, v := range []string{"4100", "4101", "4102", "4103"} {
		if h.poolLocked("port", v) {
			t.Fatalf("the waiting ticket holds port %s: partial hold", v)
		}
	}
	if h.poolLocked("browser", "1") {
		t.Fatal("the waiting ticket holds a browser: partial hold")
	}
	s := h.statusJSON()
	if len(s.Waiting) != 1 || !strings.Contains(s.Waiting[0].Reasons["local"], "waiting for pg: 2/2 held by pg1, pg2") {
		t.Fatalf("reason = %v", s.Waiting[0].Reasons)
	}
	if rc, text := h.run("why", "wide"); rc != 0 || !strings.Contains(text, "waiting for pg: 2/2 held by pg1, pg2") {
		t.Fatalf("why wide (rc=%d):\n%s", rc, text)
	}
	if rc, text := h.run("status"); rc != 0 || !strings.Contains(text, "POOLS") || !strings.Contains(text, "pg         2/2 held: ") ||
		!strings.Contains(text, "pg1 (app_wt") || !strings.Contains(text, "pg2 (app_wt") {
		t.Fatalf("status (rc=%d):\n%s", rc, text)
	}
	var pg *struct{ held, capacity int }
	for _, p := range s.Pools {
		if p.Name == "pg" {
			pg = &struct{ held, capacity int }{p.Held, p.Capacity}
		}
	}
	if pg == nil || pg.held != 2 || pg.capacity != 2 {
		t.Fatalf("snapshot pools = %+v", s.Pools)
	}
	h.touch("go1")
	if rc := want.wait(t, 30*time.Second); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, want.out)
	}
	if got := strings.Fields(h.read("env.txt")); len(got) != 4 || got[0] != "4100" || got[1] != "4101" || got[2] == "" || got[3] != "1" {
		t.Fatalf("environment = %q", h.read("env.txt"))
	}
	h.touch("go2")
	for _, p := range holders {
		p.wait(t, 20*time.Second)
	}
	for _, v := range []string{"4100", "4101"} {
		if h.poolLocked("port", v) {
			t.Fatalf("port %s still locked after the run", v)
		}
	}
	// The ledger carries what was asked and what was held.
	var found bool
	for _, r := range h.history() {
		if r.Lane == "wide" {
			found = true
			if r.Needs != "port:2,pg,browser" || !strings.HasPrefix(r.Pools, "port=4100,4101 pg=app_wt") || !strings.HasSuffix(r.Pools, " browser=1") {
				t.Fatalf("ledger needs=%q pools=%q", r.Needs, r.Pools)
			}
		}
	}
	if !found {
		t.Fatal("no ledger record")
	}
}

// A holder that dies (SIGKILL: no cleanup of its own) frees its values.
func TestPoolValuesAreReclaimedWhenTheHolderDies(t *testing.T) {
	h := newRig(t, poolConfig)
	holder := h.start("3", "--lane", "doomed", "--needs", "one", "--", "sh", "-c", h.gateFile("end"))
	h.running(1)
	if !h.poolLocked("one", "1") {
		t.Fatal("the running ticket does not hold its value")
	}
	next := h.start("3", "--lane", "next", "--needs", "one", "--", "sh", "-c", "echo got-$HEAVYQ_ONE")
	h.waiting(1)
	if err := holder.cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if rc := next.wait(t, 30*time.Second); rc != 0 || !strings.Contains(next.out.String(), "got-1") {
		t.Fatalf("rc=%d: the value of a dead holder must be handed on:\n%s", rc, next.out)
	}
	h.touch("end") // let the orphaned command end
}

func TestPoolNeedsAreValidatedBeforeQueueing(t *testing.T) {
	h := newRig(t, poolConfig)
	for name, args := range map[string][]string{
		"unknown pool":   {"3", "--needs", "nope", "--", "true"},
		"too many":       {"3", "--needs", "pg:3", "--", "true"},
		"bad count":      {"3", "--needs", "pg:0", "--", "true"},
		"bad name":       {"3", "--needs", "PG", "--", "true"},
		"twice":          {"3", "--needs", "pg", "--needs", "pg:2", "--", "true"},
		"named remote":   {"3", "--needs", "pg", "--remote", "box", "--", "true"},
		"missing value":  {"3", "--needs"},
		"empty":          {"3", "--needs", "", "--", "true"},
		"too many ports": {"3", "--needs", "port:5", "--", "true"},
	} {
		if rc, out := h.run(args...); rc != ExitUsage {
			t.Errorf("%s: rc=%d want 2\n%s", name, rc, out)
		}
	}
	if s := h.snapshot(); len(s.Waiting)+len(s.Running) != 0 {
		t.Fatal("a refused run left a ticket")
	}
	// --remote auto and --remote local are fine: needs pin the run here.
	if rc, out := h.run("3", "--needs", "pg", "--remote", "auto", "--", "true"); rc != 0 {
		t.Fatalf("--remote auto: rc=%d\n%s", rc, out)
	}
}

// Ports inside the OS ephemeral range are never handed out: a pool made only
// of them cannot be asked for.
func TestPoolPortsInsideTheEphemeralRangeAreRefused(t *testing.T) {
	r := pools.LocalEphemeralRange()
	h := newRig(t, baseConfig+fmt.Sprintf("[[pool]]\nname = \"port\"\nvalues = \"%d-%d\"\n", r.First, r.First+2))
	rc, out := h.run("3", "--needs", "port", "--", "true")
	if rc != ExitUsage || !strings.Contains(out, "ephemeral") {
		t.Fatalf("rc=%d, want 2 naming the ephemeral range:\n%s", rc, out)
	}
}

// A repository's heavyq.toml replaces the pool of the same name in the user config.
func TestRepoPoolOverridesUserPool(t *testing.T) {
	h := newRig(t, poolConfig)
	repo := gitRepo(t, h)
	toml := "[[pool]]\nname = \"pg\"\nvalues = [\"repo_db\"]\n"
	if err := os.WriteFile(filepath.Join(repo, "heavyq.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	rc, out := h.run("3", "--needs", "pg", "--", "sh", "-c", "echo db=$HEAVYQ_PG")
	if rc != 0 || !strings.Contains(out, "db=repo_db") {
		t.Fatalf("rc=%d, want the repository's pg:\n%s", rc, out)
	}
	if rc, out := h.run("3", "--needs", "pg:2", "--", "true"); rc != ExitUsage {
		t.Fatalf("the repository's pg has one value: rc=%d\n%s", rc, out)
	}
	h.cwd = h.root // outside the repository the user's pool applies
	if rc, out := h.run("3", "--needs", "pg", "--", "sh", "-c", "echo db=$HEAVYQ_PG"); rc != 0 || !strings.Contains(out, "db=app_wt") {
		t.Fatalf("rc=%d, want the user's pg:\n%s", rc, out)
	}
}

// A gate that declares needs passes them to every command it runs.
func TestGateNeedsReachTheCommand(t *testing.T) {
	h := newRig(t, poolConfig)
	repo := gitRepo(t, h)
	out := filepath.Join(h.root, "gate-env")
	toml := fmt.Sprintf("[[gate]]\nname = \"e2e\"\nrun = ['echo \"$HEAVYQ_PG $HEAVYQ_PORT_1 $HEAVYQ_PORT_2\" > %s']\nneeds = [\"pg\", \"port:2\"]\nremote = \"auto\"\n", out)
	if err := os.WriteFile(filepath.Join(repo, "heavyq.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, h, repo, "add", "heavyq.toml")
	gitIn(t, h, repo, "commit", "-q", "-m", "gate")
	if rc, text := h.run("gate", "run", "e2e"); rc != 0 {
		t.Fatalf("gate run rc=%d\n%s", rc, text)
	}
	got, _ := os.ReadFile(out)
	if f := strings.Fields(string(got)); len(f) != 3 || !strings.HasPrefix(f[0], "app_wt") || f[1] != "4100" || f[2] != "4101" {
		t.Fatalf("gate command environment = %q", got)
	}
	var ran bool
	for _, r := range h.history() {
		if r.Gate == "e2e" {
			ran = true
			if r.Needs != "pg,port:2" {
				t.Fatalf("ledger needs = %q", r.Needs)
			}
		}
	}
	if !ran {
		t.Fatal("the gate command is not in the ledger")
	}
}

// A ticket waiting for a pool does not keep a ticket that needs nothing from a
// free slot, but one that wants the same pool stays behind it.
func TestPoolBlockedTicketDoesNotBlockUnrelatedWork(t *testing.T) {
	h := newRig(t, poolConfig)
	hold := h.start("3", "--lane", "holder", "--needs", "one", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	h.start("2", "--lane", "needy", "--needs", "one", "--", "true")
	h.waiting(1)
	rc, out := h.run("5", "--lane", "free", "--", "echo", "unrelated-ran")
	if rc != 0 || !strings.Contains(out, "unrelated-ran") {
		t.Fatalf("an unrelated ticket was held up by a pool wait: rc=%d\n%s", rc, out)
	}
	h.touch("go")
	hold.wait(t, 20*time.Second)
}

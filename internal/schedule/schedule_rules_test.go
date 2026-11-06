package schedule

import (
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/metrics"
	"github.com/LysCongThanh/heavyq/internal/queue"
)

func tk(name string, light bool, cands ...string) *queue.Ticket {
	return &queue.Ticket{Name: name, Light: light, Candidates: cands}
}

func counts(h, l int) SlotCounts { return func(string) (int, int) { return h, l } }

func TestOpportunities(t *testing.T) {
	cases := []struct {
		name     string
		me       *queue.Ticket
		ahead    []*queue.Ticket
		h, l     int
		want     string // machine:kinds:head/backfill, comma separated
		blockedN int
	}{
		{"alone heavy", tk("me", false, "local"), nil, 2, 1, "local:heavy:head", 0},
		{"alone light tries light first", tk("me", true, "local"), nil, 2, 1, "local:light+heavy:head", 0},
		{"blocked by heavy ahead", tk("me", false, "local"), []*queue.Ticket{tk("a", false, "local")}, 2, 1, "", 1},
		{"light backfills past heavy", tk("me", true, "local"), []*queue.Ticket{tk("a", false, "local")}, 2, 1, "local:light:backfill", 0},
		{"light blocked by light ahead", tk("me", true, "local"), []*queue.Ticket{tk("a", true, "local")}, 2, 1, "", 1},
		{"no light slots: no backfill", tk("me", true, "local"), []*queue.Ticket{tk("a", false, "local")}, 2, 0, "", 1},
		{"ahead uses another machine", tk("me", false, "local", "box"), []*queue.Ticket{tk("a", false, "box")}, 2, 0, "local:heavy:head", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opps, blocked := Opportunities(c.me, c.ahead, counts(c.h, c.l))
			var got []string
			for _, o := range opps {
				var ks []string
				for _, k := range o.Kinds {
					ks = append(ks, k.String())
				}
				mode := "head"
				if o.Backfill {
					mode = "backfill"
				}
				got = append(got, o.Machine+":"+strings.Join(ks, "+")+":"+mode)
			}
			if g := strings.Join(got, ","); g != c.want || len(blocked) != c.blockedN {
				t.Fatalf("got %q blocked=%v, want %q blocked=%d", g, blocked, c.want, c.blockedN)
			}
		})
	}
}

func TestAhead(t *testing.T) {
	w := []*queue.Ticket{tk("a", false), tk("b", false), tk("c", false)}
	ahead, pos := Ahead(w, w[2])
	if pos != 3 || len(ahead) != 2 {
		t.Fatalf("pos=%d ahead=%d", pos, len(ahead))
	}
	if _, pos := Ahead(w, tk("zzz", false)); pos != 0 {
		t.Fatal("missing ticket must have position 0")
	}
}

func TestGate(t *testing.T) {
	m := config.MachineConfig{MaxLoadRatio: 0.75, JobMB: 3000, LightMB: 1000, ReserveMB: 2000}
	busy := func(free int, load float64) MachineState {
		return MachineState{Cfg: m, Reachable: true, Running: 1,
			Metrics: metrics.Metrics{FreeMB: free, Load1: load, Cores: 8, Known: true}}
	}
	on := GateInput{Enabled: true}
	if r := Gate(busy(10000, 1), on); r != "" {
		t.Fatalf("plenty of room should admit, got %q", r)
	}
	if r := Gate(busy(4000, 1), on); !strings.Contains(r, "RAM") {
		t.Fatalf("low RAM should block, got %q", r)
	}
	if r := Gate(busy(4000, 1), GateInput{Enabled: true, Light: true}); r != "" {
		t.Fatalf("light budget fits in 4000 MB, got %q", r)
	}
	if r := Gate(busy(10000, 6), on); !strings.Contains(r, "load") {
		t.Fatalf("load 6 >= 0.75*8 should block, got %q", r)
	}
	st := busy(10000, 1)
	st.ReservedMB = 6000
	if r := Gate(st, on); !strings.Contains(r, "held") {
		t.Fatalf("reservations must count, got %q", r)
	}
	forced := GateInput{Enabled: true, Forced: true}
	if r := Gate(busy(10000, 99), forced); r != "" {
		t.Fatalf("anti-starvation must skip the load check, got %q", r)
	}
	if r := Gate(busy(10, 99), forced); !IsRAMReason(r) {
		t.Fatalf("anti-starvation must never skip the RAM check, got %q", r)
	}
	held := busy(10000, 99)
	held.ReservedMB = 6000
	if r := Gate(held, forced); !IsRAMReason(r) || !strings.Contains(r, "held") {
		t.Fatalf("anti-starvation must count RAM held for just-started jobs, got %q", r)
	}
	if IsRAMReason("load 9.00 >= 6.0 (0.75 x 8 cores)") || IsRAMReason("unreachable") {
		t.Fatal("only the RAM check is a RAM reason")
	}
	idle := busy(10, 99)
	idle.Running = 0
	if r := Gate(idle, on); r != "" {
		t.Fatal("an idle machine always admits")
	}
	if r := Gate(MachineState{Cfg: m}, on); r != "unreachable" {
		t.Fatalf("unreachable remote, got %q", r)
	}
	unknown := busy(0, 0)
	unknown.Metrics.Known = false
	if r := Gate(unknown, on); r != "" {
		t.Fatal("unknown metrics degrade to slots only")
	}
}

func TestOutsideRepoReference(t *testing.T) {
	root := "/work/app"
	ok := [][]string{
		{"npm", "test"},
		{"go", "test", "./..."},
		{"sh", "-c", "make -j4 > /dev/null"},
		{"cat", "/work/app/package.json"},
		{"/usr/bin/env", "FOO=1", "npm", "run", "build"},
		{"node", "--max-old-space-size=4096", "x.js"},
	}
	bad := [][]string{
		{"cat", "../other/file"},
		{"sh", "-c", "cd .. && make"},
		{"ls", "~/notes"},
		{"sh", "-c", "cp $HOME/x ."},
		{"psql", "$DATABASE_URL"},
		{"sh", "-c", "$OUT/run.sh"},
		{"cat", "/etc/hosts"},
		{"cat", "/work/app-other/x"},
		{"gcc", "-I/opt/include", "x.c"},
	}
	for _, a := range ok {
		if r := OutsideRepoReference(a, root); r != "" {
			t.Errorf("%q flagged: %s", a, r)
		}
	}
	for _, a := range bad {
		if r := OutsideRepoReference(a, root); r == "" {
			t.Errorf("%q not flagged", a)
		}
	}
}

// Only a ticket with one candidate machine may be forced in: one that may run
// on several waits for whichever has room.
func TestBypassApplies(t *testing.T) {
	for _, c := range []struct {
		cands []string
		want  bool
	}{
		{nil, true},
		{[]string{"local"}, true},
		{[]string{"linuxi"}, true},
		{[]string{"local", "linuxi"}, false},
		{[]string{"mac", "local", "linuxi"}, false},
	} {
		if got := BypassApplies(&queue.Ticket{Candidates: c.cands}); got != c.want {
			t.Errorf("BypassApplies(%v) = %v, want %v", c.cands, got, c.want)
		}
	}
}

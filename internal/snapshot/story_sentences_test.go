package snapshot

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

var storyAt = time.Date(2026, 11, 1, 22, 0, 0, 0, time.UTC)

func texts(ss []Sentence) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Text
	}
	return out
}

func running(lane string, forced bool) Running {
	return Running{Ticket: Ticket{Lane: lane, Prio: 3}, Machine: "local", Slot: "heavy-1", Forced: forced}
}

func head(lane string, bypassIn float64, reasons map[string]string) Waiting {
	return Waiting{Ticket: Ticket{Lane: lane, Prio: 3}, Position: 1, BypassInS: bypassIn, Reasons: reasons}
}

func local(mod func(*Machine)) Machine {
	m := Machine{Name: "local", Reachable: true, MetricsKnown: true, TotalMB: 32768}
	if mod != nil {
		mod(&m)
	}
	return m
}

func TestStorySentences(t *testing.T) {
	const ramReason = "RAM 1200 MB < 3000 MB needed (job 2500 + reserve 500)"
	cases := []struct {
		name string
		snap Snapshot
		want []string
	}{
		{"idle", Snapshot{}, []string{"All quiet. Nothing runs, nothing waits."}},
		{"running only", Snapshot{Running: []Running{running("a", false)}},
			[]string{"1 job running and nobody waits."}},
		{"running only plural", Snapshot{Running: []Running{running("a", false), running("b", false), running("c", false)}},
			[]string{"3 jobs running and nobody waits."}},
		{"head starts on the next poll", Snapshot{
			Running: []Running{running("a", false)},
			Waiting: []Waiting{head("web", -1, map[string]string{"local": "", "box": "unreachable"}), head("api", -1, nil)},
		}, []string{"2 agents wait, and web starts on the next poll."}},
		{"ram short", Snapshot{
			Running: []Running{running("a", false)},
			Waiting: []Waiting{head("web", -1, map[string]string{"local": ramReason})},
		}, []string{"1 agent waits because local is 1.8 GB short on RAM."}},
		{"ram short counts RAM held for just-started jobs", Snapshot{
			Waiting: []Waiting{head("web", -1, map[string]string{
				"local": "RAM 4000 MB (minus 2500 MB held for just-started jobs) < 3000 MB needed"})},
		}, []string{"1 agent waits because local is 1.5 GB short on RAM."}},
		{"heavy slots busy", Snapshot{
			Waiting: []Waiting{head("web", -1, map[string]string{"local": "all heavy slots busy"})},
		}, []string{"1 agent waits because every heavy slot on local is taken."}},
		{"load over the limit", Snapshot{
			Waiting: []Waiting{head("web", -1, map[string]string{"local": "load 9.10 >= 8.0 (1.00 x 8 cores)"})},
		}, []string{"1 agent waits because local is over its load limit."}},
		{"paused machine", Snapshot{
			Waiting: []Waiting{head("web", -1, map[string]string{"local": "paused (heavyq resume to admit again)"})},
		}, []string{"1 agent waits because local is paused."}},
		{"unreachable remote in the reasons", Snapshot{
			Waiting: []Waiting{head("web", -1, map[string]string{"box": "unreachable"})},
			Machines: []Machine{local(nil),
				{Name: "box", Reachable: false}},
		}, []string{"1 agent waits because box is unreachable.", "box is unreachable."}},
		{"two machines, two causes in machine order", Snapshot{
			Waiting: []Waiting{head("web", -1, map[string]string{"local": ramReason, "box": "all heavy slots busy", "far": "load 5.00 >= 4.0 (1.00 x 4 cores)"})},
		}, []string{"1 agent waits because every heavy slot on box is taken and far is over its load limit."}},
		{"behind others only", Snapshot{
			Waiting: []Waiting{head("web", -1, map[string]string{"local": "behind api (p2)"}), head("db", -1, map[string]string{"local": "behind api (p2)"})},
		}, []string{"2 agents wait because the queue ahead is full."}},
		{"unknown reason is shown as is", Snapshot{
			Waiting: []Waiting{head("web", -1, map[string]string{"local": "something new"})},
		}, []string{"1 agent waits because local: something new."}},
		{"bypass countdown", Snapshot{
			Config:  Config{ResourceGate: true},
			Waiting: []Waiting{head("web", 130, map[string]string{"local": "all heavy slots busy"})},
		}, []string{"1 agent waits because every heavy slot on local is taken.", "web skips the load check in 2m 10s."}},
		{"bypass applies now", Snapshot{
			Config:  Config{ResourceGate: true},
			Waiting: []Waiting{head("web", 0, map[string]string{"local": ramReason})},
		}, []string{"1 agent waits because local is 1.8 GB short on RAM.", "web now skips the load check."}},
		{"no bypass sentence when the gate is off", Snapshot{
			Waiting: []Waiting{head("web", 130, map[string]string{"local": "all heavy slots busy"})},
		}, []string{"1 agent waits because every heavy slot on local is taken."}},
		{"forced admissions, one lane once", Snapshot{
			Running: []Running{running("web", true), running("api", true), running("web", true), running("db", false)},
		}, []string{"4 jobs running and nobody waits.", "web, api got in through that bypass."}},
		{"two machines down", Snapshot{
			Machines: []Machine{local(nil), {Name: "a"}, {Name: "b"}},
		}, []string{"All quiet. Nothing runs, nothing waits.", "a, b are unreachable."}},
		{"outside the queue: top and the rest", Snapshot{
			Machines: []Machine{local(nil)},
			CPU: CPU{Available: true, OutsideTotal: Usage{RSSMB: 14000}, Outside: []Usage{
				{Name: "node", Procs: 7, RSSMB: 11000}, {Name: "chrome", Procs: 3, RSSMB: 3000}}},
		}, []string{"All quiet. Nothing runs, nothing waits.", "Outside the queue, 7 node processes hold 11 GB, everything else 2.9 GB."}},
		{"outside the queue: one process, nothing else", Snapshot{
			Machines: []Machine{local(nil)},
			CPU: CPU{Available: true, OutsideTotal: Usage{RSSMB: 12000}, Outside: []Usage{
				{Name: "java", Procs: 1, RSSMB: 12000}}},
		}, []string{"All quiet. Nothing runs, nothing waits.", "Outside the queue, 1 java process holds 12 GB."}},
		{"outside the queue below the threshold stays quiet", Snapshot{
			Machines: []Machine{local(nil)},
			CPU: CPU{Available: true, OutsideTotal: Usage{RSSMB: 9000}, Outside: []Usage{
				{Name: "node", Procs: 2, RSSMB: 9000}}},
		}, []string{"All quiet. Nothing runs, nothing waits."}},
		{"outside the queue needs a known total and /proc", Snapshot{
			Machines: []Machine{local(func(m *Machine) { m.TotalMB = 0 })},
			CPU:      CPU{Available: true, OutsideTotal: Usage{RSSMB: 9000}, Outside: []Usage{{Name: "node", Procs: 2, RSSMB: 9000}}},
		}, []string{"All quiet. Nothing runs, nothing waits."}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.snap.GeneratedAt = storyAt
			got := Story(c.snap, nil, storyAt)
			if !reflect.DeepEqual(texts(got), c.want) {
				t.Fatalf("story =\n%q\nwant\n%q", texts(got), c.want)
			}
			for _, s := range got {
				var joined strings.Builder
				for _, p := range s.Parts {
					joined.WriteString(p.Text)
				}
				if joined.String() != s.Text {
					t.Fatalf("Text %q is not the concatenation of Parts %q", s.Text, joined.String())
				}
			}
		})
	}
}

func TestStorySpansCarryEmphasis(t *testing.T) {
	s := Snapshot{
		GeneratedAt: storyAt,
		Config:      Config{ResourceGate: true},
		Running:     []Running{running("api", true)},
		Machines:    []Machine{local(nil), {Name: "box"}},
		Waiting: []Waiting{head("web", 90, map[string]string{
			"local": "RAM 1200 MB < 3000 MB needed (job 2500 + reserve 500)"})},
	}
	got := Story(s, nil, storyAt)
	if len(got) != 4 {
		t.Fatalf("want 4 sentences, got %q", texts(got))
	}
	want := [][]Span{
		{{Text: "1 agent waits", Em: true}, {Text: " because "}, {Text: "local", Em: true}, {Text: " is "},
			{Text: "1.8 GB", Em: true, Num: true}, {Text: " short on RAM"}, {Text: "."}},
		{{Text: "web", Lane: "web"}, {Text: " skips the load check in "}, {Text: "1m 30s", Em: true, Num: true}, {Text: "."}},
		{{Text: "api", Lane: "api"}, {Text: " got in through that bypass."}},
		{{Text: "box is unreachable.", Hot: true}},
	}
	for i := range want {
		if !reflect.DeepEqual(got[i].Parts, want[i]) {
			t.Errorf("sentence %d parts = %+v\nwant %+v", i, got[i].Parts, want[i])
		}
	}
}

func TestStoryBypassCountdownFollowsNow(t *testing.T) {
	s := Snapshot{
		GeneratedAt: storyAt,
		Config:      Config{ResourceGate: true},
		Waiting:     []Waiting{head("web", 90, map[string]string{"local": "all heavy slots busy"})},
	}
	for _, c := range []struct {
		after time.Duration
		want  string
	}{
		{0, "web skips the load check in 1m 30s."},
		{30 * time.Second, "web skips the load check in 1m."},
		{89 * time.Second, "web skips the load check in 1s."},
		{91 * time.Second, "web now skips the load check."},
		{-time.Minute, "web skips the load check in 1m 30s."}, // clock went backwards
	} {
		got := Story(s, nil, storyAt.Add(c.after))
		if len(got) != 2 || got[1].Text != c.want {
			t.Errorf("after %s: %q, want second sentence %q", c.after, texts(got), c.want)
		}
	}
	s.GeneratedAt = time.Time{} // no timestamp: nothing elapsed
	if got := Story(s, nil, storyAt); got[1].Text != "web skips the load check in 1m 30s." {
		t.Errorf("zero GeneratedAt: %q", texts(got))
	}
}

func TestStoryInsightsSentence(t *testing.T) {
	ins := &Insights{From: storyAt.Add(-time.Hour), To: storyAt, Runs: 14, WaitP95S: 80, GaveUp: 1, CacheHits: 3, Coalesced: 2}
	got := Story(Snapshot{GeneratedAt: storyAt}, ins, storyAt)
	want := []string{"All quiet. Nothing runs, nothing waits.", "In the last 1h: 14 runs, wait p95 1m 20s, 1 gave up, 5 didn't need to run."}
	if !reflect.DeepEqual(texts(got), want) {
		t.Fatalf("story = %q", texts(got))
	}
	if got := Story(Snapshot{}, &Insights{From: storyAt.Add(-time.Hour), To: storyAt}, storyAt); len(got) != 1 {
		t.Fatalf("an empty window adds nothing: %q", texts(got))
	}
	week := &Insights{From: storyAt.Add(-7 * 24 * time.Hour), To: storyAt, Runs: 1}
	if got := Story(Snapshot{}, week, storyAt); !strings.Contains(got[1].Text, "In the last 7d: 1 run,") {
		t.Fatalf("window label: %q", texts(got))
	}
}

func TestStoryIsNeverEmptyAndMarshalsAsJSONArray(t *testing.T) {
	got := Story(Snapshot{}, nil, storyAt)
	if len(got) == 0 {
		t.Fatal("empty story")
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	const want = `[{"text":"All quiet. Nothing runs, nothing waits.","parts":[{"text":"All quiet.","em":true},{"text":" Nothing runs, nothing waits."}]}]`
	if string(b) != want {
		t.Fatalf("json = %s", b)
	}
}

func TestFormatHelpersMatchTheDashboard(t *testing.T) {
	for in, want := range map[float64]string{0: "0s", 44.6: "45s", 59.4: "59s", 60: "1m", 185: "3m 05s", 3600: "1h 00m", 7440: "2h 04m", -5: "0s"} {
		if got := dur(in); got != want {
			t.Errorf("dur(%v) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[int]string{-1: "0 MB", 640: "640 MB", 1023: "1023 MB", 1024: "1.0 GB", 3300: "3.2 GB", 10239: "10.0 GB", 10240: "10 GB", 24576: "24 GB"} {
		if got := mb(in); got != want {
			t.Errorf("mb(%d) = %q, want %q", in, got, want)
		}
	}
	if n, ok := ramShort("RAM 100 MB < 3000 MB needed"); !ok || n != 2900 {
		t.Errorf("ramShort = %d, %v", n, ok)
	}
	if n, ok := ramShort("RAM 9000 MB < 3000 MB needed"); !ok || n != 0 {
		t.Errorf("ramShort clamps at 0: %d, %v", n, ok)
	}
	if _, ok := ramShort("load 9 >= 8"); ok {
		t.Error("a load reason is not a RAM reason")
	}
}

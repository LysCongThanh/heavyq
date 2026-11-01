package snapshot

import (
	"reflect"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/history"
)

func TestLaneLines(t *testing.T) {
	snap := Snapshot{
		GeneratedAt: storyAt,
		Config:      Config{ResourceGate: true},
		Running: []Running{
			{Ticket: Ticket{Lane: "api", Prio: 3, Pid: 11, Cmd: "go test ./..."}, Machine: "local", Slot: "heavy-1", RunS: 123, Forced: true},
			{Ticket: Ticket{Lane: "other", Prio: 3, Pid: 12, Cmd: "make"}, Machine: "local", Slot: "heavy-2", RunS: 5},
		},
		Waiting: []Waiting{
			{Ticket: Ticket{Lane: "other", Prio: 2, Pid: 20, Cmd: "npm test", WaitS: 4}, Position: 1, BypassInS: -1,
				Reasons: map[string]string{"local": "behind x (p1)"}},
			{Ticket: Ticket{Lane: "api", Prio: 3, Pid: 21, Cmd: "env API_TOKEN=abc go vet", WaitS: 45}, Position: 2, BypassInS: 130,
				Reasons: map[string]string{
					"local": "RAM 1200 MB < 3000 MB needed (job 2500 + reserve 500)",
					"box":   "all heavy slots busy",
				}},
		},
	}
	last := &history.Record{End: storyAt.Add(-185 * time.Second), Machine: "local", Lane: "api", RunSecs: 41, Cmd: "go test ./... --token abc123456"}
	got := LaneLines(snap, "api", last, storyAt.Add(10*time.Second))
	want := []string{
		"lane api: 1 running, 1 waiting.",
		"  running on local/heavy-1 for 2m 13s (p3, pid 11): go test ./... [admitted by the anti-starvation bypass]",
		"  waiting #2 for 55s (p3, pid 21): env API_TOKEN=*** go vet",
		"    box: all heavy slots busy",
		"    local: RAM 1200 MB < 3000 MB needed (job 2500 + reserve 500)",
		"    api skips the RAM and load checks in 2m.",
		"last run passed in 41s on local, 3m 15s ago: go test ./... --token ***",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lines =\n%q\nwant\n%q", got, want)
	}
}

func TestLaneLinesUnknownLane(t *testing.T) {
	got := LaneLines(Snapshot{GeneratedAt: storyAt}, "ghost", nil, storyAt)
	want := []string{"lane ghost: not running and not waiting.", "no run of this lane in the history yet."}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %q", got)
	}
}

func TestDescribeRun(t *testing.T) {
	end := storyAt.Add(-90 * time.Second)
	cases := []struct {
		name string
		rec  history.Record
		want string
	}{
		{"passed", history.Record{End: end, Machine: "local", RunSecs: 8, Cmd: "a"}, "last run passed in 8s on local, 1m 30s ago: a"},
		{"failed", history.Record{End: end, Machine: "box", RunSecs: 70, Exit: 2, Cmd: "a"}, "last run failed with exit 2 after 1m 10s on box, 1m 30s ago: a"},
		{"gave up", history.Record{End: end, WaitSecs: 600, Exit: 75, Result: history.OutcomeGaveUp, Cmd: "a"}, "last run gave up after waiting 10m, 1m 30s ago: a"},
		{"cancelled", history.Record{End: end, Result: history.OutcomeCancelled, Exit: 78, Cmd: "a"}, "last run was cancelled by an operator, 1m 30s ago: a"},
		{"interrupted", history.Record{End: end, Machine: "local", Result: history.OutcomeInterrupted, Exit: 130, Cmd: "a"}, "last run was interrupted on local, 1m 30s ago: a"},
		{"cache hit", history.Record{End: end, Cache: "hit", Cmd: "a"}, "last run reused a cached result, 1m 30s ago: a"},
		{"coalesced", history.Record{End: end, Cache: "coalesced", Cmd: "a"}, "last run shared the result of an identical run, 1m 30s ago: a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := describeRun(c.rec, storyAt); got != c.want {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}

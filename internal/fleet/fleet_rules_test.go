package fleet

import (
	"strings"
	"testing"
	"time"
)

// The red split: what is the machine's or the queue's fault (infra) and what is the code's.
func TestExitClassification(t *testing.T) {
	c := NewClassifier([]int{97, 124})
	for _, tc := range []struct {
		exit int
		want string
	}{
		{0, ClassOK},
		{1, ClassCode}, {2, ClassCode}, {101, ClassCode}, {143, ClassCode},
		{127, ClassInfra},                   // command not found: the machine lacks a tool
		{137, ClassInfra},                   // killed (OOM killer, kill -9)
		{255, ClassInfra},                   // ssh dropped
		{97, ClassInfra}, {124, ClassInfra}, // the queue's own infra codes, from the config
	} {
		if got := c.Class(tc.exit); got != tc.want {
			t.Errorf("exit %d: %s, want %s", tc.exit, got, tc.want)
		}
	}
	if NewClassifier(nil).Class(97) != ClassCode {
		t.Error("an exit is infra only when built in or configured")
	}
}

func TestOverloadRule(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    Sample
		want string // "" = not overloaded; else a word every reason list must contain
	}{
		{"idle", Sample{Cores: 16, Load1: 4, FreeMB: 20000, TotalMB: 32000}, ""},
		{"busy but fine", Sample{Cores: 18, Load1: 26.9, FreeMB: 9000, TotalMB: 32000}, ""},
		{"load 45 on 18 cores", Sample{Cores: 18, Load1: 45, FreeMB: 9000, TotalMB: 32000}, "load"},
		{"load 56 on 10 cores", Sample{Cores: 10, Load1: 56, FreeMB: 9000, TotalMB: 16000}, "load"},
		{"exactly the ratio", Sample{Cores: 10, Load1: 15, FreeMB: 9000, TotalMB: 16000}, "load"},
		{"ram under 10%", Sample{Cores: 16, Load1: 2, FreeMB: 1500, TotalMB: 32000}, "RAM"},
		{"ram floor without a total", Sample{Cores: 16, Load1: 2, FreeMB: 900}, "RAM"},
		{"memory stalls", Sample{Cores: 16, Load1: 2, FreeMB: 9000, TotalMB: 32000, Pressure: &Pressure{MemFullAvg10: 12}}, "pressure"},
		{"cpu pressure alone is not overload", Sample{Cores: 16, Load1: 2, FreeMB: 9000, TotalMB: 32000, Pressure: &Pressure{CPUSomeAvg10: 90}}, ""},
		{"unknown cores", Sample{Load1: 50, FreeMB: 9000, TotalMB: 32000}, ""},
	} {
		why := Overload(tc.s)
		if (len(why) > 0) != (tc.want != "") || (tc.want != "" && !strings.Contains(strings.Join(why, ";"), tc.want)) {
			t.Errorf("%s: %v, want %q", tc.name, why, tc.want)
		}
	}
}

func TestSummarizeWindow(t *testing.T) {
	var runs []Run
	for i := 1; i <= 20; i++ { // waits 1..20 minutes, runs 15 minutes each
		r := Run{Queue: "bashq", Machine: "box", WaitS: float64(i * 60), RunS: 900, Class: ClassOK}
		switch i {
		case 3, 4:
			r.Exit, r.Class = 255, ClassInfra
		case 5:
			r.Exit, r.Class = 1, ClassCode
		}
		runs = append(runs, r)
	}
	s := Summarize(runs, 10*time.Hour)
	if s.Runs != 20 || s.RunHours != 5 || s.PerHour != 2 {
		t.Fatalf("counts: %+v", s)
	}
	if s.WaitP50S != 600 || s.WaitP90S != 1080 || s.WaitP95S != 1140 || s.RunP50S != 900 {
		t.Fatalf("percentiles (nearest rank): %+v", s)
	}
	if s.Red != 3 || s.RedInfra != 2 || s.RedCode != 1 || s.InfraRate != 0.1 || s.CodeRate != 0.05 {
		t.Fatalf("red split: %+v", s)
	}
	if z := Summarize(nil, time.Hour); z.Runs != 0 || z.WaitP50S != 0 || z.InfraRate != 0 {
		t.Fatalf("empty window: %+v", z)
	}
}

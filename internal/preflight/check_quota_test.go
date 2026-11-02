package preflight

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func pct(f float64) *float64 { return &f }

func TestCheckQuota(t *testing.T) {
	cases := []struct {
		name  string
		q     *Quota
		hours float64
		want  []string
	}{
		{"unknown quota is silent", nil, 8, nil},
		{"plenty", &Quota{FiveHourPct: pct(10), SevenDayPct: pct(20)}, 8, nil},
		{"five hour nearly spent", &Quota{FiveHourPct: pct(85)}, 2, []string{"warn:5 hour window is 85% used"}},
		{"half spent matters for a long run", &Quota{FiveHourPct: pct(55)}, 8, []string{"warn:55% used and the run lasts 8 h"}},
		{"half spent is fine for a short run", &Quota{FiveHourPct: pct(55)}, 2, nil},
		{"half spent, length unstated", &Quota{FiveHourPct: pct(55)}, 0, nil},
		{"week nearly spent", &Quota{SevenDayPct: pct(90)}, 1, []string{"warn:7 day quota is 90% used"}},
		{"week mostly spent, day-long run", &Quota{SevenDayPct: pct(75)}, 12, []string{"warn:7 day quota is 75% used and the run lasts 12 h"}},
		{"both", &Quota{FiveHourPct: pct(95), SevenDayPct: pct(95)}, 1, []string{"warn:5 hour", "warn:7 day"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { assertFindings(t, CheckQuota(c.q, c.hours), c.want) })
	}
}

func TestReadQuota(t *testing.T) {
	common := t.TempDir()
	dir := filepath.Join(common, "heavyq", "sessions")
	if q := ReadQuota(common, time.Now()); q != nil {
		t.Fatalf("no directory: %+v", q)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string, age time.Duration) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-age)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	write("old.meta.json", `{"rate_limits":{"five_hour_pct":10}}`, 10*time.Minute)
	write("new.meta.json", `{"model":"x","rate_limits":{"five_hour_pct":81.5,"seven_day_pct":40}}`, time.Minute)
	write("broken.meta.json", `{nope`, 0)
	write("other.json", `{"rate_limits":{"five_hour_pct":99}}`, 0)
	q := ReadQuota(common, time.Now())
	if q == nil || q.FiveHourPct == nil || *q.FiveHourPct != 81.5 || *q.SevenDayPct != 40 {
		t.Fatalf("newest valid meta wins: %+v", q)
	}
	if q := ReadQuota(common, time.Now().Add(3*time.Hour)); q != nil {
		t.Errorf("a stale reading is unknown: %+v", q)
	}
	write("nolimits.meta.json", `{"model":"x"}`, 0)
	if q := ReadQuota(common, time.Now()); q == nil || *q.FiveHourPct != 81.5 {
		t.Errorf("a meta file without rate_limits is skipped: %+v", q)
	}
}

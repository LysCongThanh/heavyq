package sessionmeta

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)

const sampleStatusline = `{"session_id":"abc-123","cwd":"/w/app","version":"2.1.90","model":{"id":"claude-opus-5","display_name":"Opus"},
"workspace":{"current_dir":"/w/app"},"rate_limits":{"five_hour":{"used_percentage":61.5,"resets_at":1738425600},"seven_day":{"used_percentage":20}}}`

func TestParseStatuslineReadsTheDocumentedFields(t *testing.T) {
	p, err := ParseStatusline([]byte(sampleStatusline))
	if err != nil {
		t.Fatal(err)
	}
	m := p.Meta
	if m.Session != "abc-123" || m.Model != "claude-opus-5" || m.Version != "2.1.90" || p.Dir != "/w/app" ||
		m.FiveHourPct == nil || *m.FiveHourPct != 61.5 || m.SevenDayPct == nil || *m.SevenDayPct != 20 {
		t.Fatalf("%+v dir=%q", m, p.Dir)
	}
	if m.Agent() != "claude-code/2.1.90" {
		t.Errorf("agent %q", m.Agent())
	}
	if m.FiveHourResetsAt == nil || m.FiveHourResetsAt.Unix() != 1738425600 || m.SevenDayResetsAt != nil {
		t.Errorf("resets_at is Unix seconds, and only the windows that say it have one: %v %v", m.FiveHourResetsAt, m.SevenDayResetsAt)
	}
}

func TestResetTimeRefusesWhatIsNotAUnixTime(t *testing.T) {
	bad := []float64{0, -5, 1e13}
	for _, v := range bad {
		if ResetTime(&v) != nil {
			t.Errorf("%v is not a reset time", v)
		}
	}
	if ResetTime(nil) != nil {
		t.Error("nil stays nil")
	}
}

func TestParseStatuslineIsLenientAboutEverythingButTheSession(t *testing.T) {
	p, err := ParseStatusline([]byte(`{"session_id":"s1","model":{"id":"m"},"rate_limits":{"five_hour":{"used_percentage":150},"seven_day":null}}`))
	if err != nil || p.Meta.FiveHourPct != nil || p.Meta.SevenDayPct != nil || p.Meta.Version != "" || p.Meta.Agent() != "claude-code" {
		t.Fatalf("an out-of-range or absent window is simply unknown: %+v %v", p, err)
	}
	for _, bad := range []string{``, `{`, `[]`, `{"model":{"id":"m"}}`, `{"session_id":"a b/c"}`, `{"session_id":"../x/y"}`} {
		if _, err := ParseStatusline([]byte(bad)); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestRecordInStoresReadsBackAndSkipsUnchangedWrites(t *testing.T) {
	common := t.TempDir()
	p, _ := ParseStatusline([]byte(sampleStatusline))
	m, ok, err := RecordIn(common, p.Meta, now)
	if err != nil || !ok || !m.Updated.Equal(now) {
		t.Fatalf("%+v %v %v", m, ok, err)
	}
	got, err := Read(common, "abc-123")
	if err != nil || got == nil || got.Model != "claude-opus-5" || got.Schema != schema {
		t.Fatalf("%+v %v", got, err)
	}
	path := filepath.Join(Dir(common), "abc-123.meta.json")
	before, _ := os.Stat(path)
	m2, _, _ := RecordIn(common, p.Meta, now.Add(10*time.Second)) // a 300 ms refresh cadence: nothing to say
	after, _ := os.Stat(path)
	if !m2.Updated.Equal(now) || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("an unchanged record younger than a minute is not rewritten: %+v", m2)
	}
	m3, _, _ := RecordIn(common, p.Meta, now.Add(2*time.Minute))
	if !m3.Updated.Equal(now.Add(2 * time.Minute)) {
		t.Fatal("an old record is refreshed even when unchanged")
	}
	changed := p.Meta
	changed.Model = "claude-sonnet-5"
	if m4, _, _ := RecordIn(common, changed, now.Add(3*time.Minute)); m4.Model != "claude-sonnet-5" {
		t.Fatalf("a changed model is written at once: %+v", m4)
	}
}

func TestRecordModelSwitchKeepsBoundedChangePointsAndSurvivesStatuslineWrites(t *testing.T) {
	common := t.TempDir()
	p, _ := ParseStatusline([]byte(sampleStatusline))
	_, _, _ = RecordIn(common, p.Meta, now)
	if err := RecordModelSwitch(common, "abc-123", "", "claude-sonnet-5", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	m, _ := Read(common, "abc-123")
	if m.Model != "claude-sonnet-5" || len(m.Switches) != 1 || m.Switches[0].From != "claude-opus-5" || m.Switches[0].To != "claude-sonnet-5" {
		t.Fatalf("%+v", m)
	}
	// The status line reports the new model; the change points stay.
	next := p.Meta
	next.Model = "claude-sonnet-5"
	if got, _, _ := RecordIn(common, next, now.Add(2*time.Minute)); len(got.Switches) != 1 {
		t.Fatalf("a status-line write must keep the change points: %+v", got)
	}
	for i := 0; i < 40; i++ {
		_ = RecordModelSwitch(common, "abc-123", "", "m", now.Add(time.Duration(i)*time.Hour))
	}
	if m, _ := Read(common, "abc-123"); len(m.Switches) != maxSwitches {
		t.Fatalf("%d change points kept, want %d", len(m.Switches), maxSwitches)
	}
	if err := RecordModelSwitch(common, "abc-123", "", "", now); err == nil {
		t.Error("a switch without a target is refused")
	}
	if err := RecordModelSwitch(common, "../x", "", "m", now); err == nil {
		t.Error("an unsafe session id is refused")
	}
}

func TestLatestIsTheNewestAndSkipsCorruptFiles(t *testing.T) {
	common := t.TempDir()
	if Latest(common) != nil {
		t.Fatal("no records, no latest")
	}
	_, _, _ = RecordIn(common, Meta{Session: "old", Model: "m1"}, now)
	_, _, _ = RecordIn(common, Meta{Session: "new", Model: "m2"}, now.Add(time.Hour))
	_ = os.WriteFile(filepath.Join(Dir(common), "bad.meta.json"), []byte("{"), 0o600)
	if m := Latest(common); m == nil || m.Session != "new" {
		t.Fatalf("%+v", m)
	}
}

func TestReadOfAnUnsafeOrMissingSessionIsNil(t *testing.T) {
	common := t.TempDir()
	for _, s := range []string{"", "nope", "a/b", ".."} {
		if m, err := Read(common, s); m != nil || err != nil {
			t.Errorf("%q: %v %v", s, m, err)
		}
	}
}

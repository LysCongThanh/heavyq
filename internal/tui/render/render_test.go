package render

import (
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestThemeFromEnv(t *testing.T) {
	utf := map[string]string{"LANG": "en_US.UTF-8"}
	with := func(extra map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range utf {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	tests := []struct {
		name  string
		env   map[string]string
		ascii bool
		want  Theme
	}{
		{"colour utf-8", utf, false, Theme{LevelColor, false}},
		{"NO_COLOR any value", with(map[string]string{"NO_COLOR": "1"}), false, Theme{LevelMono, false}},
		{"empty NO_COLOR is ignored", with(map[string]string{"NO_COLOR": ""}), false, Theme{LevelColor, false}},
		{"dumb terminal", with(map[string]string{"TERM": "dumb"}), false, Theme{LevelNone, true}},
		{"C locale gets ASCII", map[string]string{"LANG": "C"}, false, Theme{LevelColor, true}},
		{"no locale gets ASCII", map[string]string{}, false, Theme{LevelColor, true}},
		{"LC_ALL beats LANG", map[string]string{"LC_ALL": "C", "LANG": "en_US.UTF-8"}, false, Theme{LevelColor, true}},
		{"utf8 spelling", map[string]string{"LC_CTYPE": "de_DE.utf8"}, false, Theme{LevelColor, false}},
		{"--ascii flag", utf, true, Theme{LevelColor, true}},
	}
	for _, tc := range tests {
		if got := ThemeFromEnv(envOf(tc.env), tc.ascii); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestModes(t *testing.T) {
	for _, n := range ModeNames() {
		m, ok := ParseMode(n)
		if !ok || m.String() != n {
			t.Errorf("ParseMode(%q) = %v, %v", n, m, ok)
		}
	}
	if _, ok := ParseMode("nope"); ok {
		t.Error("ParseMode accepted an unknown mode")
	}
	m := ModeFull
	for i := 0; i < len(ModeNames()); i++ {
		m = m.Next()
	}
	if m != ModeFull {
		t.Errorf("cycling through every mode ended on %v, want full", m)
	}
}

func TestRowsAndFilter(t *testing.T) {
	s := loadFixture(t, "busy")
	all := Rows(s, "")
	if len(all) != 3+2+2+3 {
		t.Fatalf("rows = %d, want 3 exceptions + 4 tickets + 3 machines", len(all))
	}
	web := Rows(s, "WEB")
	var tickets, machines int
	for _, r := range web {
		switch r.Kind {
		case RowTicket:
			tickets++
			if r.Lane != "web" || !r.Waiting || r.Ticket == "" {
				t.Errorf("unexpected ticket row %+v", r)
			}
		case RowMachine:
			machines++
		}
	}
	if tickets != 1 || machines != 3 {
		t.Errorf("filter web: %d tickets, %d machines; machines always stay", tickets, machines)
	}
	for _, m := range []struct {
		mode Mode
		kind RowKind
	}{{ModeFocus, RowException}, {ModeLanes, RowTicket}, {ModeMachines, RowMachine}} {
		rows := RowsIn(s, "", m.mode)
		if len(rows) == 0 {
			t.Fatalf("%v: no rows", m.mode)
		}
		for _, r := range rows {
			if r.Kind != m.kind {
				t.Errorf("%v shows a %v row", m.mode, r.Kind)
			}
		}
	}
	if RowsIn(s, "", ModeLine) != nil {
		t.Error("the line mode has nothing to select")
	}
	if i := Find(all, "m:mac"); i < 0 || all[i].Machine != "mac" {
		t.Error("Find did not locate the mac row")
	}
}

func TestExceptionRowsCarryTheirAction(t *testing.T) {
	s := loadFixture(t, "busy")
	for _, r := range Rows(s, "") {
		if r.Kind == RowException && r.Exception.Action != nil && r.Exception.Action.Kind == "raise" && r.Ticket == "" {
			t.Error("a raise exception must expose its ticket")
		}
	}
	d := loadFixture(t, "degraded")
	for _, r := range Rows(d, "") {
		if r.Kind == RowException && r.Exception.Action != nil && r.Exception.Action.Kind == "resume" && r.Machine != "linuxi" {
			t.Errorf("resume exception machine = %q", r.Machine)
		}
	}
}

func TestStripIsOneLineAndPlainWithoutSGR(t *testing.T) {
	s := loadFixture(t, "busy")
	line := Strip(s, fixtureView(), Options{Cols: 200, Theme: themeNone})
	if strings.ContainsAny(line, "\n\x1b") {
		t.Errorf("strip is not one plain line: %q", line)
	}
	for _, want := range []string{"hq", "2 run", "2 wait", "3 need you", "linuxi"} {
		if !strings.Contains(line, want) {
			t.Errorf("strip %q lacks %q", line, want)
		}
	}
}

func TestASCIIThemeNeverEmitsNonASCII(t *testing.T) {
	th := Theme{Level: LevelNone, ASCII: true}
	for _, in := range []string{"load ≥ 9 …", "café", "日本", "é"} {
		for _, b := range []byte(th.txt(in)) {
			if b >= 0x80 {
				t.Errorf("txt(%q) = %q has a non-ASCII byte", in, th.txt(in))
				break
			}
		}
	}
}

package fleet

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// A machine whose probe sent its numbers but not its process table is reachable and partial in every
// rendering: its load and RAM are shown, the overload rule still runs, and what is missing is said.
func TestPartialMachineIsShownNotUnreachable(t *testing.T) {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	why := "no top consumers and attribution (timed out after 30s)"
	r := Assemble(Inputs{Now: now, Since: now.Add(-time.Hour), Machines: []MachineInput{{Name: "slow", Role: RoleObserveOnly,
		Probe: &Probe{OS: "Linux", Cores: 18, Load1: 70, FreeMB: 80000, TotalMB: 96000, Partial: why}}}})
	m := r.Machines[0]
	if !m.Reachable || m.Error != "" || m.Partial != why || !m.Overload || m.Cores != 18 || len(m.Top) != 0 {
		t.Fatalf("machine: %+v", m)
	}

	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct{ Machines []map[string]any }
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if got := doc.Machines[0]; got["partial"] != why || got["reachable"] != true || got["error"] != nil || got["top"] == nil {
		t.Fatalf("json machine: %v", got)
	}

	var text bytes.Buffer
	RenderText(&text, r)
	md := string(RenderMarkdown(r))
	agent := RenderAgent(r)
	for name, out := range map[string]string{"text": text.String(), "markdown": md, "agent": agent} {
		if !strings.Contains(out, "partial") || !strings.Contains(out, "timed out after 30s") || strings.Contains(out, "unreachable") {
			t.Errorf("%s:\n%s", name, out)
		}
	}
	if strings.Contains(md, "Outside every queue: 0") {
		t.Errorf("markdown shows an RSS it did not measure:\n%s", md)
	}

	// a whole probe says nothing of the kind
	r = Assemble(Inputs{Now: now, Since: now.Add(-time.Hour), Machines: []MachineInput{{Name: "calm", Role: RoleLocal,
		Probe: &Probe{OS: "Linux", Cores: 4, Load1: 1, FreeMB: 4000, TotalMB: 8000}}}})
	if raw, _ := json.Marshal(r); bytes.Contains(raw, []byte(`"partial"`)) {
		t.Fatalf("json of a whole probe: %s", raw)
	}
}

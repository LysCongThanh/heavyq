package control

import "testing"

const tk = "3-01762011288000000000-50122-wt-billing"

func TestCancelPrioPause(t *testing.T) {
	m := Mailbox{Dir: t.TempDir() + "/ctl"}
	if m.CancelRequested(tk) {
		t.Fatal("no request yet")
	}
	if err := m.RequestCancel(tk); err != nil || !m.CancelRequested(tk) {
		t.Fatalf("cancel: %v", err)
	}
	if err := m.RequestPrio(tk, 0); err == nil {
		t.Fatal("prio 0 must be refused")
	}
	if err := m.RequestPrio(tk, 2); err != nil {
		t.Fatal(err)
	}
	if p, ok := m.TakePrio(tk); !ok || p != 2 {
		t.Fatalf("TakePrio = %d %v", p, ok)
	}
	if _, ok := m.TakePrio(tk); ok {
		t.Fatal("a priority request is consumed once")
	}
	if err := m.RequestCancel(tk); err != nil {
		t.Fatal(err)
	}
	if !m.CancelRequested("7" + tk[1:]) {
		t.Fatal("a cancel must survive a priority change of the ticket")
	}
	m.Forget(tk)
	if m.CancelRequested(tk) {
		t.Fatal("Forget must clear the cancel request")
	}
	if err := m.SetPaused("build-box", true); err != nil || !m.Paused("build-box") {
		t.Fatalf("pause: %v", err)
	}
	if err := m.SetPaused("build-box", false); err != nil || m.Paused("build-box") {
		t.Fatalf("resume: %v", err)
	}
	if err := m.SetPaused("build-box", false); err != nil {
		t.Fatal("resuming twice is fine")
	}
}

func TestRejectsPathTricks(t *testing.T) {
	m := Mailbox{Dir: t.TempDir()}
	for _, bad := range []string{"../x", "", "3-1-2-a/../../b", "a b"} {
		if m.RequestCancel(bad) == nil || m.RequestPrio(bad, 2) == nil {
			t.Errorf("ticket %q accepted", bad)
		}
	}
	for _, bad := range []string{"../x", "", ".hidden", "a/b"} {
		if m.SetPaused(bad, true) == nil {
			t.Errorf("machine %q accepted", bad)
		}
	}
}

package control

import (
	"os"
	"testing"
	"time"
)

func TestTargetRequestAndAnswer(t *testing.T) {
	m := Mailbox{Dir: t.TempDir() + "/ctl"}
	if _, ok := m.TakeTarget(tk); ok {
		t.Fatal("no request yet")
	}
	if err := m.RequestTarget(tk, "box,local"); err != nil {
		t.Fatal(err)
	}
	// The owner reads it under its current name, whatever its priority digit says now.
	if w, ok := m.TakeTarget("7" + tk[1:]); !ok || w != "box,local" {
		t.Fatalf("TakeTarget = %q %v", w, ok)
	}
	if _, ok := m.TakeTarget(tk); ok {
		t.Fatal("a target request is consumed once")
	}
	if _, ok := m.TargetAnswer(tk); ok {
		t.Fatal("no answer before the owner answers")
	}
	if err := m.AnswerTarget(tk, "this machine cannot run it"); err != nil {
		t.Fatal(err)
	}
	if why, ok := m.TargetAnswer(tk); !ok || why != "this machine cannot run it" {
		t.Fatalf("TargetAnswer = %q %v", why, ok)
	}
	if _, ok := m.TargetAnswer(tk); ok {
		t.Fatal("an answer is read once")
	}

	// A new request drops an answer to an older one.
	if err := m.AnswerTarget(tk, ""); err != nil {
		t.Fatal(err)
	}
	if err := m.RequestTarget(tk, "local"); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.TargetAnswer(tk); ok {
		t.Fatal("an answer to an older request survived a new one")
	}
	// The ticket ends: its request goes, its answer stays for the requester
	// (a moved ticket may run and end before the requester looks).
	m.Forget(tk)
	if _, ok := m.TakeTarget(tk); ok {
		t.Fatal("Forget must clear the target request")
	}
	if err := m.AnswerTarget(tk, ""); err != nil {
		t.Fatal(err)
	}
	m.Forget(tk)
	if why, ok := m.TargetAnswer(tk); !ok || why != "" {
		t.Fatalf("answer after Forget = %q %v, want the applied answer", why, ok)
	}
}

// Answers nobody reads (the dashboard does not wait for one) are swept by a
// later request once they are older than answerTTL; fresh ones are kept.
func TestUnreadTargetAnswersAreSwept(t *testing.T) {
	m := Mailbox{Dir: t.TempDir()}
	old, fresh := tk, "3-01762011288000000001-50123-wt-other"
	for _, id := range []string{old, fresh} {
		if err := m.AnswerTarget(id, ""); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-answerTTL - time.Minute)
	if err := os.Chtimes(m.path(key(old)+".target-done"), past, past); err != nil {
		t.Fatal(err)
	}
	if err := m.RequestTarget("3-01762011288000000002-50124-wt-third", "local"); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.TargetAnswer(old); ok {
		t.Fatal("an answer older than answerTTL was kept")
	}
	if _, ok := m.TargetAnswer(fresh); !ok {
		t.Fatal("a fresh answer was swept")
	}
}

func TestTargetRequestRefusesBadInput(t *testing.T) {
	m := Mailbox{Dir: t.TempDir()}
	for _, bad := range []string{"../x", "", "3-1-2-a/../../b"} {
		if m.RequestTarget(bad, "local") == nil || m.AnswerTarget(bad, "") == nil {
			t.Errorf("ticket %q accepted", bad)
		}
	}
	for _, bad := range []string{"", "a b", "../x", "box,", ",box", "box\nlocal", "-x"} {
		if m.RequestTarget(tk, bad) == nil {
			t.Errorf("target %q accepted", bad)
		}
	}
}

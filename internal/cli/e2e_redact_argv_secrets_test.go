package cli

import (
	"strings"
	"testing"
	"time"
)

// A secret passed as one argv element with spaces (the shell already dropped
// the quotes), curl -u user:password and a mysql -pPASSWORD never reach the
// waiting ticket that status --json serves nor the ledger record, as a whole
// or in part. Values are fake test fixtures.
func TestE2ESecretArgvValuesAreHiddenInStatusAndLedger(t *testing.T) {
	h := newRig(t, baseConfig) // 1 heavy slot
	hold := h.start("4", "--lane", "holder", "--", "sh", "-c", h.gateFile("go"))
	h.running(1)
	argv := []string{"true", "--password", "correct horse battery", "-u", "admin:staple-pw", "mysql", "-pmysql-pw"}
	waiter := h.start(append([]string{"4", "--lane", "secret", "--"}, argv...)...)
	h.waiting(1)
	leaks := func(where, cmd string) {
		t.Helper()
		for _, s := range []string{"correct", "horse", "battery", "staple-pw", "mysql-pw"} {
			if strings.Contains(cmd, s) {
				t.Fatalf("%s leaks %q: %q", where, s, cmd)
			}
		}
		if !strings.Contains(cmd, "--password ***") || !strings.Contains(cmd, "admin:***") || !strings.Contains(cmd, "-p***") {
			t.Fatalf("%s is not the redacted command: %q", where, cmd)
		}
	}
	s := h.statusJSON()
	if len(s.Waiting) != 1 {
		t.Fatalf("waiting = %+v", s.Waiting)
	}
	leaks("status --json", s.Waiting[0].Cmd)

	h.touch("go")
	hold.wait(t, 30*time.Second)
	if rc := waiter.wait(t, 30*time.Second); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, waiter.out)
	}
	leaks("the ledger", h.lastRecord().Cmd)
}

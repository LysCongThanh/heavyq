package cli

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/receipts"
)

// Smoke: real runs through the queue, then the receipts of the binary. The
// exact summary counts them, and --share, in text and in JSON, is rounded and
// carries neither the lane nor the command.
func TestE2EReceipts(t *testing.T) {
	h := newRig(t, baseConfig)
	for i := 0; i < 2; i++ {
		if rc, out := h.run("4", "--lane", "canary-lane-zq", "--", "echo", "canary-cmd-zq"); rc != 0 {
			t.Fatalf("run %d: rc=%d\n%s", i, rc, out)
		}
	}

	rc, out := h.run("receipts")
	if rc != 0 || !strings.Contains(out, "last 7 days") || !regexp.MustCompile(`heavy runs +2\n`).MatchString(out) {
		t.Fatalf("receipts: rc=%d\n%s", rc, out)
	}

	rc, out = h.run("receipts", "--day", "--json")
	var exact receipts.Receipts
	if err := json.Unmarshal([]byte(out), &exact); rc != 0 || err != nil || exact.Runs != 2 || exact.Lanes != 1 {
		t.Fatalf("receipts --json: rc=%d err=%v\n%s", rc, err, out)
	}

	rc, card := h.run("receipts", "--month", "--share")
	if rc != 0 || !strings.Contains(card, "┌") || !strings.Contains(card, "last 30 days") ||
		!strings.Contains(card, "2 runs in total") || !strings.Contains(card, "heavyq ") {
		t.Fatalf("receipts --share: rc=%d\n%s", rc, card)
	}
	if strings.Count(card, "\n") > 12 {
		t.Errorf("card must be at most 12 lines:\n%s", card)
	}
	rc, js := h.run("receipts", "--share", "--json")
	var s receipts.Share
	if err := json.Unmarshal([]byte(js), &s); rc != 0 || err != nil || s.Text == "" || s.Window != "last 7 days" {
		t.Fatalf("receipts --share --json: rc=%d err=%v\n%s", rc, err, js)
	}
	for _, secret := range []string{"canary-lane-zq", "canary-cmd-zq", h.root} {
		if strings.Contains(card, secret) || strings.Contains(js, secret) {
			t.Errorf("--share leaks %q:\n%s\n%s", secret, card, js)
		}
	}

	if rc, out := h.run("receipts", "--day", "--week"); rc != ExitUsage || !strings.Contains(out, "only one of") {
		t.Errorf("two windows: rc=%d\n%s", rc, out)
	}
	if rc, _ := h.run("receipts", "extra"); rc != ExitUsage {
		t.Errorf("a stray argument: rc=%d, want usage", rc)
	}
}

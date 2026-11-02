package snapshot

import (
	"strings"
	"testing"
	"time"
)

func TestBlockedItemsBecomeCriticalTowerCards(t *testing.T) {
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	s := Snapshot{Items: []LaneItems{
		{Lane: "ui", Repo: "app", Items: []ItemChip{
			{ID: "login-form", State: ItemBlocked, Needs: "design sign-off", At: at},
			{ID: "login-api", State: ItemClaimed},
			{ID: "docs", State: ItemProven, Commit: "abc1234"},
		}},
	}}
	ex := ComputeExceptions(s, nil, at)
	if len(ex) != 1 {
		t.Fatalf("only the blocked item is an exception: %+v", ex)
	}
	e := ex[0]
	if e.Kind != KindBlocked || e.Severity != SeverityCritical || e.Lane != "ui" || e.Evidence.Item != "login-form" ||
		e.Evidence.Detail != "design sign-off" || !e.Evidence.At.Equal(at) ||
		!strings.Contains(e.Text, "ui is blocked on item `login-form`: needs design sign-off") ||
		e.Action == nil || e.Action.Kind != ActionRun || e.Action.Command != "heavyq item claim login-form" {
		t.Fatalf("%+v action=%+v", e, e.Action)
	}

	// The id changes with the need, so a dismissal does not hide a new need.
	s.Items[0].Items[0].Needs = "legal review"
	if again := ComputeExceptions(s, nil, at); len(again) != 1 || again[0].ID == e.ID {
		t.Fatalf("a new need must be a new exception: %q vs %+v", e.ID, again)
	}
}

func TestBlockedRanksBetweenLoopsAndStarvedLanes(t *testing.T) {
	now := time.Now()
	s := Snapshot{
		Items: []LaneItems{{Lane: "ui", Items: []ItemChip{{ID: "x", State: ItemBlocked, Needs: "n"}}}},
		Gates: []LaneGate{{Lane: "api", Gate: "ready", Tree: "t", State: GateFailed,
			Failed: []GateFailure{{Cmd: "go test", At: now, Count: 1}}}},
		Machines: []Machine{{Name: "box", Reachable: false}},
	}
	var kinds []string
	for _, e := range ComputeExceptions(s, nil, now) {
		kinds = append(kinds, e.Kind)
	}
	if got := strings.Join(kinds, ","); got != "red_gate,blocked,machine" {
		t.Fatalf("order = %s", got)
	}
}

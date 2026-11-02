package itemstatus

import (
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/anchor"
	"github.com/LysCongThanh/heavyq/internal/lease"
)

var t0 = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

func liveLease(lane string) *lease.Lease {
	return &lease.Lease{Item: "x", Lane: lane, Session: "s1", Expires: t0.Add(10 * time.Minute)}
}

func expiredLease(lane string) *lease.Lease {
	return &lease.Lease{Item: "x", Lane: lane, Session: "s1", Expires: t0.Add(-time.Minute)}
}

func provenEv() Evidence {
	return Evidence{Declared: true, Proven: true, Commit: "abc1234", At: t0.Add(-time.Minute)}
}

func TestDeriveTable(t *testing.T) {
	older := &Run{Commit: "old1234", At: t0.Add(-time.Hour)}
	staleLease := liveLease("a")
	staleLease.Stale = true // pid evidence says the owner is gone
	cases := []struct {
		name      string
		in        Input
		wantState string
		check     func(t *testing.T, st Status)
	}{
		{"open", Input{Proof: Evidence{Declared: true}}, Open, nil},
		{"open without proof", Input{Proof: NoProof()}, Open, nil},
		{"claimed", Input{Lease: liveLease("api"), Proof: Evidence{Declared: true}}, Claimed, func(t *testing.T, st Status) {
			if st.Lane != "api" || st.Session != "s1" || !st.Expires.Equal(t0.Add(10*time.Minute)) {
				t.Fatalf("claimed = %+v", st)
			}
		}},
		{"expired lease is open", Input{Lease: expiredLease("api")}, Open, func(t *testing.T, st Status) {
			if st.Lane != "" || !st.Expires.IsZero() {
				t.Fatalf("an expired lease must hold nothing: %+v", st)
			}
		}},
		{"pid-dead lease is open", Input{Lease: staleLease}, Open, nil},
		{"proven", Input{Proof: provenEv()}, Proven, func(t *testing.T, st Status) {
			if st.Commit != "abc1234" || st.At.IsZero() {
				t.Fatalf("proven = %+v", st)
			}
		}},
		{"proven keeps the claiming lane", Input{Lease: liveLease("api"), Proof: provenEv()}, Proven, func(t *testing.T, st Status) {
			if st.Lane != "api" {
				t.Fatalf("proven = %+v", st)
			}
		}},
		{"proven beats a block and a ticked box", Input{Item: anchor.Item{Checked: true, Needs: "x"}, Mark: Mark{Blocked: true}, Proof: provenEv()}, Proven, nil},
		{"stale", Input{Proof: Evidence{Declared: true, Older: older}}, Stale, func(t *testing.T, st Status) {
			if st.Commit != "old1234" || !st.At.Equal(older.At) {
				t.Fatalf("stale = %+v", st)
			}
		}},
		{"claimed beats stale", Input{Lease: liveLease("api"), Proof: Evidence{Declared: true, Older: older}}, Claimed, nil},
		{"ticked without proof", Input{Item: anchor.Item{Checked: true}, Proof: NoProof()}, ClaimedDoneUnproven, func(t *testing.T, st Status) {
			if st.Reason == "" {
				t.Fatal("want a reason")
			}
		}},
		{"ticked, proof never ran", Input{Item: anchor.Item{Checked: true}, Proof: Evidence{Declared: true}}, ClaimedDoneUnproven, nil},
		{"ticked, proof red", Input{Item: anchor.Item{Checked: true}, Proof: Evidence{Declared: true, Failed: true, Reason: "failed on this code: go test"}}, ClaimedDoneUnproven, func(t *testing.T, st Status) {
			if st.Reason != "ticked, but unproven: failed on this code: go test" {
				t.Fatalf("reason = %q", st.Reason)
			}
		}},
		{"ticked, proven earlier on another tree", Input{Item: anchor.Item{Checked: true}, Proof: Evidence{Declared: true, Older: older}}, Stale, nil},
		{"ticked beats a lease", Input{Item: anchor.Item{Checked: true}, Lease: liveLease("api"), Proof: NoProof()}, ClaimedDoneUnproven, func(t *testing.T, st Status) {
			if !st.Expires.IsZero() {
				t.Fatalf("no expiry on a ticked item: %+v", st)
			}
		}},
		{"plan-file item marked done, unproven", Input{Item: anchor.Item{ReadOnly: true}, Mark: Mark{Done: true}, Proof: NoProof()}, ClaimedDoneUnproven, nil},
		{"done mark is ignored for the anchor file", Input{Mark: Mark{Done: true}, Proof: NoProof()}, Open, nil},
		{"blocked by event", Input{Mark: Mark{Blocked: true, Needs: "needs: design sign-off", Lane: "ui"}}, Blocked, func(t *testing.T, st Status) {
			if st.Needs != "needs: design sign-off" || st.Lane != "ui" {
				t.Fatalf("blocked = %+v", st)
			}
		}},
		{"blocked by file needs", Input{Item: anchor.Item{Needs: "design sign-off"}}, Blocked, func(t *testing.T, st Status) {
			if st.Needs != "design sign-off" {
				t.Fatalf("blocked = %+v", st)
			}
		}},
		{"block without a need says so", Input{Mark: Mark{Blocked: true}}, Blocked, func(t *testing.T, st Status) {
			if st.Needs != "unspecified" {
				t.Fatalf("blocked = %+v", st)
			}
		}},
		{"blocked beats a stale proof", Input{Mark: Mark{Blocked: true, Needs: "n"}, Proof: Evidence{Declared: true, Older: older}}, Blocked, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.in.Now = t0
			if c.in.Item.ID == "" {
				c.in.Item.ID = "x"
			}
			st := Derive(c.in)
			if st.State != c.wantState || st.ID != "x" {
				t.Fatalf("state = %q, want %q (%+v)", st.State, c.wantState, st)
			}
			if c.check != nil {
				c.check(t, st)
			}
		})
	}
}

func TestClaimable(t *testing.T) {
	cases := []struct {
		state     string
		fileNeeds bool
		byName    bool
		next      bool
	}{
		{Open, false, true, true},
		{Stale, false, true, true},
		{Blocked, false, true, false},
		{Blocked, true, false, false},
		{Claimed, false, false, false},
		{Proven, false, false, false},
		{ClaimedDoneUnproven, false, false, false},
	}
	for _, c := range cases {
		byName, next := Claimable(Status{State: c.state}, c.fileNeeds)
		if byName != c.byName || next != c.next {
			t.Errorf("%s (file needs %v) = %v, %v; want %v, %v", c.state, c.fileNeeds, byName, next, c.byName, c.next)
		}
	}
}

func TestDoneItemsAreNeverHandedOutAgain(t *testing.T) {
	older := &Run{Commit: "old1234", At: t0.Add(-time.Hour)}
	for _, in := range []Input{
		{Item: anchor.Item{ID: "x", Checked: true}, Proof: Evidence{Declared: true, Older: older}}, // ticked by heavyq, code moved on
		{Item: anchor.Item{ID: "x", Checked: true}, Proof: NoProof()},
		{Item: anchor.Item{ID: "x", ReadOnly: true}, Mark: Mark{Done: true}, Proof: Evidence{Declared: true, Older: older}},
	} {
		in.Now = t0
		st := Derive(in)
		if byName, next := Claimable(st, false); !st.Done || byName || next {
			t.Errorf("%+v: state %s done=%v claimable=%v,%v", in.Item, st.State, st.Done, byName, next)
		}
	}
	if st := Derive(Input{Item: anchor.Item{ID: "x"}, Proof: provenEv(), Now: t0}); st.Done {
		t.Fatalf("an unticked item is not done: %+v", st)
	}
}

func TestMarksClaimUnblocks(t *testing.T) {
	ev := func(op, item, lane string) lease.Event { return lease.Event{Op: op, Item: item, Lane: lane, At: t0} }
	block := ev(lease.OpBlock, "a", "ui")
	block.Needs = "design"
	done := ev(lease.OpDone, "c", "api")
	done.Commit = "abc1234"
	m := Marks([]lease.Event{
		ev(lease.OpClaim, "a", "ui"), block,
		ev(lease.OpClaim, "b", "ui"), ev(lease.OpBlock, "b", "ui"), ev(lease.OpClaim, "b", "api"), // unblocked again
		done,
		ev(lease.OpClaim, "d", "ui"), ev(lease.OpDone, "d", "ui"), ev(lease.OpReclaim, "d", "api"), // reopened
		ev(lease.OpRelease, "a", "ui"), ev(lease.OpRenew, "a", "ui"), // change nothing
	})
	if a := m["a"]; !a.Blocked || a.Needs != "design" || a.Lane != "ui" {
		t.Fatalf("a = %+v", a)
	}
	if _, ok := m["b"]; ok {
		t.Fatalf("a claim must clear the block: %+v", m["b"])
	}
	if c := m["c"]; !c.Done || c.Commit != "abc1234" {
		t.Fatalf("c = %+v", c)
	}
	if _, ok := m["d"]; ok {
		t.Fatalf("a reclaim must clear the done: %+v", m["d"])
	}
}

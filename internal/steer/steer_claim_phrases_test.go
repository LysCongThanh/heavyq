package steer

import "testing"

func TestClaimsDone(t *testing.T) {
	yes := []string{
		"All tests pass.",
		"I fixed the parser. All tests pass now.",
		"All the tests are passing.",
		"all unit tests passed",
		"Implemented it. The tests pass.",
		"Everything is wired up.\n\nTests are now passing.",
		"ALL TESTS PASS!",
	}
	no := []string{
		"",
		"Done.",
		"Everything works.",
		"The tests don't pass yet.",
		"Not all tests pass.",
		"Tests fail on the auth package.",
		"I need to make sure the tests pass.",
		"Once the tests pass we can merge.",
		"If all tests pass, I will commit.",
		"Should the tests pass, ship it.",
		"I will run the tests next.",
	}
	for _, s := range yes {
		if !ClaimsDone(s) {
			t.Errorf("must match: %q", s)
		}
	}
	for _, s := range no {
		if ClaimsDone(s) {
			t.Errorf("must not match: %q", s)
		}
	}
}

func TestClaimsDoneLooksAtTheEndOfAHugeMessage(t *testing.T) {
	long := make([]byte, 50000)
	for i := range long {
		long[i] = 'x'
	}
	if !ClaimsDone(string(long) + ". All tests pass.") {
		t.Error("a claim at the end of a long message must match")
	}
	if ClaimsDone("All tests pass. " + string(long)) {
		t.Error("only the tail of a huge message is read")
	}
}

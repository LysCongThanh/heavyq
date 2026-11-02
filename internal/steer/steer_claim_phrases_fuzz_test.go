package steer

import (
	"strings"
	"testing"
)

// FuzzClaimsDone: the last assistant message is free text from a model. The matcher must not panic or take long,
// and a message without the word "pass" or "green" can never be a claim.
func FuzzClaimsDone(f *testing.F) {
	for _, s := range []string{"All tests pass.", "tests don't pass", "", "TESTS PASSED!\n\nall green", "\x00\xff", strings.Repeat("tests ", 5000)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, msg string) {
		if ClaimsDone(msg) && !strings.Contains(strings.ToLower(msg), "pass") && !strings.Contains(strings.ToLower(msg), "green") {
			t.Fatalf("claim without a claim word: %q", msg)
		}
	})
}

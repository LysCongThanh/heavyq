package steer

import (
	"regexp"
	"strings"
)

// Claim-vs-ledger looks for two phrases only, on purpose. A matcher that
// guesses ("looks good", "should work") would nag on prose; these two are what
// an agent writes when it says the work is verified:
//
//   - "all tests pass" (also "all tests are passing", "all tests passed")
//   - "tests pass" / "tests are passing" / "tests passed"
//
// and "everything works" is not matched. The match is on the last assistant
// message only, lower-cased, and it must be a statement: a negation, a failure
// word or a conditional within the same sentence cancels it ("tests don't
// pass", "once the tests pass", "make sure the tests pass").
var (
	claimAllTests = regexp.MustCompile(`\ball (the )?(unit |integration |e2e )?tests?\b[^.!?\n]{0,20}\b(pass|passes|passed|passing|green)\b`)
	claimTests    = regexp.MustCompile(`\b(tests?|test suite|the suite)\b[^.!?\n]{0,12}\b(pass|passes|passed|passing)\b`)
	// claimHedge cancels a match: a negation, a failure word, or a sentence
	// about something to do rather than something done. Missing a real claim
	// costs nothing; nagging on prose costs trust.
	claimHedge = regexp.MustCompile(`n't|\b(not|never|cannot|fail|fails|failed|failing|failure|failures|unless|if|once|until|when|whether|should|before|need to|needs to|want|wants|will|would|make sure|ensure|verify|check|so that)\b`)
)

// ClaimsDone reports whether the final message of an agent states that the
// tests pass. It is deliberately conservative: two phrases, one sentence, no
// negation or hedge in that sentence.
func ClaimsDone(msg string) bool {
	if len(msg) > 20000 {
		msg = msg[len(msg)-20000:] // the end of the message is the claim
	}
	msg = strings.ToLower(msg)
	for _, sentence := range splitSentences(msg) {
		if !claimAllTests.MatchString(sentence) && !claimTests.MatchString(sentence) {
			continue
		}
		if claimHedge.MatchString(sentence) {
			continue
		}
		return true
	}
	return false
}

func splitSentences(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == '!' || r == '?' || r == '\n' })
}

package stance

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxPromptScan bounds how much of a prompt is searched: the keyword is typed
// by a person, a pasted log further down is not a request.
const maxPromptScan = 16 << 10

// MatchKeyword reports the first keyword of keywords that appears in prompt:
// case-insensitive, on whole words (the characters on both sides are not
// letters or digits), in any script ("đột phá" matches like "breakthrough").
// Accents are significant, and a decomposed (NFD) spelling of a precomposed
// keyword does not match: no normalisation tables ship with the stdlib.
// The keyword returned is the stance's own text, never the user's.
func MatchKeyword(prompt string, keywords []string) (string, bool) {
	if len(prompt) > maxPromptScan {
		cut := maxPromptScan
		for cut > 0 && !utf8.RuneStart(prompt[cut]) {
			cut-- // never cut inside a character
		}
		prompt = prompt[:cut]
	}
	hay := strings.ToLower(prompt)
	for _, kw := range keywords {
		needle := strings.ToLower(strings.TrimSpace(kw))
		if needle != "" && containsWord(hay, needle) {
			return kw, true
		}
	}
	return "", false
}

// containsWord is strings.Contains with word boundaries around the match.
func containsWord(hay, needle string) bool {
	for from := 0; from <= len(hay)-len(needle); {
		i := strings.Index(hay[from:], needle)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(needle)
		before, _ := utf8.DecodeLastRuneInString(hay[:start])
		after, _ := utf8.DecodeRuneInString(hay[end:])
		if (start == 0 || !isWordRune(before)) && (end == len(hay) || !isWordRune(after)) {
			return true
		}
		from = start + 1
	}
	return false
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) }

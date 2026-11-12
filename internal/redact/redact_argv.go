package redact

import (
	"regexp"
	"strings"
)

// shellScriptFlag is the flag before a script argument: sh -c, bash -lc,
// python -c. Such an element is shell or program text, not one value.
var shellScriptFlag = regexp.MustCompile(`^-[A-Za-z]*c$`)

// Argv hides likely secrets in a command given as argv and returns it joined
// with spaces, the form heavyq stores and shows.
//
// The shell has already removed the quotes, so `--password "a b c"` arrives
// as one element "a b c": joined and passed to Command, only "a" would be
// hidden. Argv keeps such an element one value by standing NUL in for its
// spaces while Command runs (Command treats NUL as part of a value), then puts
// the spaces back where nothing was hidden. A script after -c is shell text
// and is redacted as such. Argv has no shell operators, so a mysql -pPASSWORD
// is hidden even after an element with ';', '&' or '|' in it. The result is a
// fixed point of Command.
func Argv(argv []string) string {
	parts := make([]string, len(argv))
	copy(parts, argv)
	encoded := false
	if !anyHasNUL(argv) { // NUL cannot be in a real argv; never confuse one with a space
		for i, a := range argv {
			if strings.Contains(a, " ") && (i == 0 || !shellScriptFlag.MatchString(argv[i-1])) {
				parts[i], encoded = strings.ReplaceAll(a, " ", "\x00"), true
			}
		}
	}
	// after the encoding, which rewrites parts from argv: masked first, a
	// -pPASSWORD with a space would get its raw value back
	maskMysqlArgvPasswords(parts)
	s := Command(strings.Join(parts, " "))
	if encoded {
		s = strings.ReplaceAll(s, "\x00", " ")
	}
	return s
}

func anyHasNUL(argv []string) bool {
	for _, a := range argv {
		if strings.IndexByte(a, 0) >= 0 {
			return true
		}
	}
	return false
}

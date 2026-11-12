package toolcheck

import (
	"path"
	"regexp"
	"strings"
	"unicode"
)

// shells run the script given with -c; anything else they run is a script file, which is not read.
var shells = map[string]bool{"sh": true, "bash": true, "dash": true, "zsh": true, "ksh": true}

// prefixes run the rest of their words as a command when no option follows them.
var prefixes = map[string]bool{"nice": true, "nohup": true, "time": true, "exec": true}

var (
	assignRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	shellOptRe = regexp.MustCompile(`^[-+][A-Za-z]+$`)
)

const (
	// metachars end the part of a script that is read word by word: quotes, expansions, separators,
	// redirects, grouping, globs and comments.
	metachars = "\"'`$\\;&|<>(){}*?[#\n"
	// wordEnds are the metachars a command word may touch and still be a whole word (`psql<x`, `go;`).
	wordEnds = ";&|<>\n"
	maxDepth = 4
)

// FromArgv lists the plain command names argv runs, outermost first and each once: argv[0], then what it
// runs when that is obvious. env (with -i and NAME=VALUE), nice/nohup/time/exec without options and a
// shell's -c script (its first command, leading assignments skipped, read only up to the first quote,
// expansion or separator) are seen through; the gate wrapper is the caller's to unwrap (cli knows
// proof.Argv, which this package must not import: repoconfig depends on it). A path (./x, /usr/bin/x) is
// not a tool name: it lives in the repository or names its own location. A word that cannot be read with
// certainty ends the list, so a name is listed only when the run really starts it.
func FromArgv(argv []string) []string { return Union(collect(argv, nil, 0)) }

func collect(argv, out []string, depth int) []string {
	if len(argv) == 0 || depth > maxDepth {
		return out
	}
	word, base := argv[0], path.Base(argv[0])
	switch {
	case word == base && Valid(word):
		out = append(out, word)
	case word == base, !Valid(base), !shells[base] && base != "env":
		return out // not a name, or a path that is not a wrapper heavyq can see through
	}
	rest := argv[1:]
	switch {
	case base == "env":
		return collect(skipEnv(rest), out, depth+1)
	case prefixes[base]:
		if len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
			return out
		}
		return collect(rest, out, depth+1)
	case shells[base]:
		if script, ok := shellScript(rest); ok {
			return collect(scriptWords(script), out, depth+1)
		}
	}
	return out
}

// skipEnv drops what env reads before the command: -i, - and NAME=VALUE. Any other option ends the list
// (its value could be taken for the command).
func skipEnv(args []string) []string {
	for ; len(args) > 0; args = args[1:] {
		a := args[0]
		switch {
		case a == "-i" || a == "-" || a == "--ignore-environment" || assignRe.MatchString(a):
		case strings.HasPrefix(a, "-"):
			return nil
		default:
			return args
		}
	}
	return nil
}

// shellScript returns the script a shell runs with -c; ok is false when the shell runs a script file or its
// options cannot be read with certainty.
func shellScript(args []string) (string, bool) {
	cmdMode, i := false, 0
	for ; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			i++
			break
		}
		if len(a) < 2 || (a[0] != '-' && a[0] != '+') {
			break
		}
		switch {
		case a == "--rcfile" || a == "--init-file":
			return "", false // a long option with a value
		case strings.HasPrefix(a, "--"):
		case !shellOptRe.MatchString(a):
			return "", false
		default:
			cmdMode = cmdMode || (a[0] == '-' && strings.ContainsRune(a, 'c'))
			if strings.ContainsAny(a, "oO") {
				i++ // -o NAME, -O NAME
			}
		}
	}
	if !cmdMode || i >= len(args) {
		return "", false
	}
	return args[i], true
}

// scriptWords returns the words of the first simple command of script, without leading assignments, as
// far as they can be read without a shell: up to the first metachar. A word that runs on into that
// metachar is not a whole word and is dropped.
func scriptWords(script string) []string {
	prefix := script
	i := strings.IndexAny(script, metachars)
	if i >= 0 {
		prefix = script[:i]
	}
	words := strings.Fields(prefix)
	if i > 0 && len(words) > 0 && !unicode.IsSpace(rune(prefix[len(prefix)-1])) && !strings.ContainsRune(wordEnds, rune(script[i])) {
		words = words[:len(words)-1]
	}
	for len(words) > 0 && assignRe.MatchString(words[0]) {
		words = words[1:]
	}
	return words
}

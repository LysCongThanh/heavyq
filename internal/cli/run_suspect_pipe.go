package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// warnShellPipe says, once, when the command is `sh -c '<string with a pipe>'`:
// the shell's exit status is the last stage's, so a failing or missing first
// stage still gives exit 0.
func warnShellPipe(w io.Writer, argv []string) {
	script, ok := shellScript(argv)
	if !ok || !hasPipe(script) || strings.Contains(strings.Join(argv, " "), "pipefail") {
		return
	}
	fmt.Fprintln(w, "heavyq: warning: this shell command has a pipe, so its exit status is the last stage's and a failing first stage is not seen; "+
		"use `bash -o pipefail -c`, or no pipe (heavyq keeps the full log)")
}

// shellScript returns the string of `<shell> [-flags] -c <string>`.
func shellScript(argv []string) (string, bool) {
	if len(argv) < 3 {
		return "", false
	}
	switch filepath.Base(argv[0]) {
	case "sh", "bash", "dash", "zsh", "ksh", "ash":
	default:
		return "", false
	}
	for i := 1; i < len(argv)-1; i++ {
		a := argv[i]
		if a == "-o" || a == "+o" {
			i++ // takes an option name
			continue
		}
		if !strings.HasPrefix(a, "-") || strings.HasPrefix(a, "--") {
			return "", false
		}
		if strings.Contains(a, "c") {
			return argv[i+1], true
		}
	}
	return "", false
}

// hasPipe reports a `|` of the shell outside quotes that is not part of `||`.
func hasPipe(s string) bool {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '\\':
			i++
		case c == '\'' || c == '"':
			quote = c
		case c == '|':
			if i+1 < len(s) && s[i+1] == '|' {
				i++ // ||
				continue
			}
			return true
		}
	}
	return false
}

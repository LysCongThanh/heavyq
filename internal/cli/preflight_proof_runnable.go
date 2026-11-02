package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// shellBuiltins are words `sh -c` runs without finding a program.
var shellBuiltins = map[string]bool{
	"cd": true, "test": true, "[": true, "true": true, "false": true, "echo": true, "exit": true,
	"export": true, "set": true, ".": true, ":": true, "exec": true, "eval": true, "!": true,
	"(": true, "{": true, "if": true, "for": true, "while": true, "case": true, "printf": true, "read": true,
}

var (
	envAssignWord = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	plainWord     = regexp.MustCompile(`^[A-Za-z0-9_./+-]+$`)
)

// missingProgram says why the first program of a proof command cannot run from
// root ("" when it can, or when it cannot be told): a command that is not
// installed is a proof that fails at 3 a.m. for a reason known now. It only
// judges a plain first word; anything shell-shaped is left to the shell.
func missingProgram(cmd, root string) string {
	words := strings.Fields(cmd)
	for len(words) > 0 && envAssignWord.MatchString(words[0]) {
		words = words[1:]
	}
	if len(words) == 0 || shellBuiltins[words[0]] || !plainWord.MatchString(words[0]) {
		return ""
	}
	prog := words[0]
	if strings.Contains(prog, "/") {
		path := prog
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		if info, err := os.Stat(path); err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
			return fmt.Sprintf("%s is not an executable file", prog)
		}
		return ""
	}
	if _, err := exec.LookPath(prog); err != nil {
		return fmt.Sprintf("`%s` is not installed (not on PATH)", prog)
	}
	return ""
}

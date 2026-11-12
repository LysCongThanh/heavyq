package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// normalizeHookBinary checks hook.binary, which the hook writes shell-quoted,
// as ONE word, at the front of every rewritten command. A quoted "~/" is never
// expanded and a value with arguments is one word naming no file, so either
// would make every rewritten agent command exit 127. "~/" is expanded here,
// and what remains must be a command name found on PATH or an absolute path
// (a relative path would depend on the agent's working directory).
func normalizeHookBinary(b string) (string, error) {
	if b == "" {
		return "", nil
	}
	if rest, ok := strings.CutPrefix(b, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("hook.binary %q: %w", b, err)
		}
		b = filepath.Join(home, rest)
	}
	switch {
	case strings.ContainsAny(b, HookBinaryUnsafe) || strings.ContainsFunc(b, unicode.IsControl):
		return "", fmt.Errorf("hook.binary %q contains shell metacharacters or control characters", b)
	case strings.HasPrefix(b, "~"):
		return "", fmt.Errorf(`hook.binary %q: only a leading "~/" (your home) is expanded`, b)
	case filepath.IsAbs(b):
		return b, nil
	case strings.ContainsFunc(b, unicode.IsSpace) || strings.Contains(b, "/"):
		return "", fmt.Errorf("hook.binary %q must be a command name on PATH or an absolute path, with no arguments", b)
	}
	return b, nil
}

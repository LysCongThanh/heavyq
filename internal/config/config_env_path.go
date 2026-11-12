package config

import (
	"fmt"
	"strings"
)

// validateEnvPath checks a remote's env_path: directories separated by ':'.
// The value is handed to the remote shell script, so quotes, expansions and
// control characters are refused rather than guessed at. A leading "~/" is
// allowed; the script expands it to the remote home.
func validateEnvPath(p string) error {
	if p == "" {
		return nil
	}
	bad := func(r rune) bool { return r < ' ' || r == 0x7f || strings.ContainsRune(EnvPathUnsafe, r) }
	if strings.Trim(p, ":") == "" || strings.IndexFunc(p, bad) >= 0 {
		return fmt.Errorf("env_path %q must be directories separated by ':' without quotes, '$', backticks or control characters", p)
	}
	return nil
}

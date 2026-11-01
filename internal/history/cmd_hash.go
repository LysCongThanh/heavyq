package history

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// CmdHash identifies the exact command a run executed: the sha256, in hex, of
// the argv joined with NUL. A NUL cannot occur in an argument, so two
// different argvs never share the same input. Compute it from the argv as
// given, before redaction and truncation: Record.Cmd is a redacted, cut-off
// display form that long or secret-bearing commands cannot be matched on.
func CmdHash(argv []string) string {
	sum := sha256.Sum256([]byte(strings.Join(argv, "\x00")))
	return hex.EncodeToString(sum[:])
}

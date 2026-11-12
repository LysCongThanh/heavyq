package cli

import (
	"fmt"
	"io"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// printExitCanary lists the remotes whose stored exit canary verdict says they lose exit codes (see
// run_exit_canary.go). Informational: it never changes doctor's exit code, and a config that does not load
// (already reported) lists nothing.
func printExitCanary(w io.Writer, cfgPath string, explicit bool) {
	cfg, err := config.Load(cfgPath, explicit)
	if err != nil {
		return
	}
	for _, m := range cfg.Remotes {
		if note := untrustedNote(m); note != "" {
			fmt.Fprintf(w, "remote: %s; its runs prove nothing until the canary passes again: %s\n", note, nextUntrusted)
		}
	}
}

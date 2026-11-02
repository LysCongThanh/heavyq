package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/steer"
)

// printBriefDelivery is the delivery canary check of `heavyq doctor`: the
// SessionStart hook puts a random token in the brief and records it with the
// transcript path Claude Code gave it; here the latest token is looked for in
// that transcript. Best effort and optional: without a repository, a recorded
// token or a readable transcript it says nothing (or "not checked") and never
// changes doctor's exit code.
func printBriefDelivery(w io.Writer) {
	wd, err := os.Getwd()
	if err != nil {
		return
	}
	common, err := gitutil.CommonDir(wd)
	if err != nil {
		return
	}
	_, st, ok := steer.OpenSessions(common).LatestCanary()
	if !ok {
		return
	}
	v, err := steer.CheckCanary(st.Transcript, st.Canary)
	fmt.Fprintln(w, steer.CanaryText(v, st.Canary, err))
}

package cli

import (
	"os"
	"time"

	"github.com/LysCongThanh/heavyq/internal/flow"
	"github.com/LysCongThanh/heavyq/internal/flow/collect"
	"github.com/LysCongThanh/heavyq/internal/flowstate"
	"github.com/LysCongThanh/heavyq/internal/runspan"
)

// flowGHEnv set to "off" keeps the flow state from ever calling gh (offline
// work, or a repository that is not on GitHub). The git collector is local and
// stays on.
const flowGHEnv = "HEAVYQ_FLOW_GH"

// gatherFlowExtras fills what Build takes beyond the ledger, the leases and the
// hooks, each of it optional: the baseline the constraint finder learned (read
// back from the state it wrote last), the open run span, and the spans of the
// git and gh collectors. Whatever cannot be read is left out; it is logged for
// the log's sake and never fails the refresh.
func gatherFlowExtras(in *flow.Input, common, root string, from time.Time) {
	if prev, err := flowstate.Read(common); err == nil && prev != nil {
		in.Baseline = prev.Baseline
	}
	if cur, err := runspan.Current(common, in.Now); err == nil && cur != nil {
		in.Run = runInfoOf(*cur)
	}
	if len(in.Items) == 0 {
		return
	}
	ids := make([]string, 0, len(in.Items))
	for _, f := range in.Items {
		ids = append(ids, f.Status.ID)
	}
	collector := collect.All(collect.Options{Items: ids, Common: common, NoGH: os.Getenv(flowGHEnv) == "off"})
	ext, err := collector(root, from, in.Now)
	if err != nil {
		hookLog("flow: collectors: %v", err)
	}
	in.External = ext
}

// runInfoOf is the "run" of state.json for a run span.
func runInfoOf(s runspan.Span) *flow.RunInfo {
	return &flow.RunInfo{ID: s.ID, Started: s.Started.UTC(), ExpectedEnd: s.ExpectedEnd, Goal: oneLine(s.Goal, maxGoalChars),
		Items: len(s.Items), Parallel: s.Parallel}
}

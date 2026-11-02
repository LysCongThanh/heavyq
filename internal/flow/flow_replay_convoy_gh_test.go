package flow_test

import (
	"context"
	"encoding/json"
	"time"

	"github.com/LysCongThanh/heavyq/internal/flow/collect"
)

// convoyGH is gh as it would have answered at the instant now, for the plan: a
// run that had not finished is in progress and has not been updated since, a
// pull request is open until the minute it landed. It is the fake behind the
// collector's GHRunner, so the collector parses real gh JSON.
func convoyGH(c convoy, now time.Time) collect.GHRunner {
	return func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "pr" {
			var prs []map[string]any
			for _, p := range c.PRs {
				if p.Start.After(now) {
					continue
				}
				state, merged := "OPEN", "0001-01-01T00:00:00Z"
				if !p.Land.After(now) {
					state, merged = "MERGED", p.Land.UTC().Format(time.RFC3339)
				}
				prs = append(prs, map[string]any{"number": len(prs) + 1, "headRefName": p.Branch(), "state": state, "isDraft": false,
					"createdAt": p.Start.UTC().Format(time.RFC3339), "mergedAt": merged, "closedAt": "0001-01-01T00:00:00Z"})
			}
			return json.Marshal(prs)
		}
		var runs []map[string]any
		for _, p := range c.PRs {
			for _, r := range p.Runs {
				if r.Start.After(now) {
					continue
				}
				status, conclusion, updated := "completed", "success", r.End
				switch {
				case r.End.After(now):
					status, conclusion, updated = "in_progress", "", now
				case r.Cancelled:
					conclusion = "cancelled"
				}
				runs = append(runs, map[string]any{"headBranch": r.Branch, "headSha": r.SHA, "event": "pull_request", "status": status,
					"conclusion": conclusion, "createdAt": r.Start.UTC().Format(time.RFC3339), "startedAt": r.Start.UTC().Format(time.RFC3339),
					"updatedAt": updated.UTC().Format(time.RFC3339)})
			}
		}
		return json.Marshal(runs)
	}
}

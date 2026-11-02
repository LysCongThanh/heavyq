package collect

import (
	"os/exec"
	"time"

	"github.com/LysCongThanh/heavyq/internal/flow"
)

// GH is the collector of ci_wait and merge_wait, from the workflow runs and the
// pull requests of the repository as gh reports them.
//
//   - ci_wait: every workflow run on an item's branch, from its start to its
//     end (to the horizon while it is still going). Each push and each rerun
//     is a run, so a convoy of reruns shows as what it is.
//   - merge_wait: from the moment the newest commit's runs were all green until
//     the pull request was merged (until the horizon while it is not). Drafts
//     and closed pull requests wait for nothing.
//
// It is optional in every way. Without gh on the PATH, or when gh is not logged
// in, offline, too slow (ghTimeout) or on a repository that is not on GitHub,
// it yields nothing, or the spans of the last good answer. The answer is cached
// under the common dir and gh is asked at most once per GHInterval, failures
// included. Spans that are still open are cut at the horizon: GHInterval after
// the last good answer, so stale data never claims a wait that has since ended.
func GH(o Options) flow.Collector {
	return func(repo string, from, to time.Time) ([]flow.External, error) {
		if len(o.Items) == 0 {
			return nil, nil
		}
		run := o.GH
		if run == nil {
			if _, err := exec.LookPath(ghProgram); err != nil {
				return nil, nil // no gh: the states simply never appear
			}
			run = execGH
		}
		snap, err := snapshotGH(o, run, repo)
		if snap == nil {
			return nil, err
		}
		horizon := snap.FetchedAt.Add(GHInterval)
		if to.Before(horizon) {
			horizon = to
		}
		return ghSpans(snap, o.Items, from, horizon), err
	}
}

// ghSpans derives the spans from one answer of gh. Open spans end at horizon;
// spans that ended before from are left out.
func ghSpans(s *ghSnapshot, items []string, from, horizon time.Time) []flow.External {
	var out []flow.External
	add := func(item string, st flow.State, start, end time.Time) {
		if end.After(horizon) {
			end = horizon
		}
		if end.After(start) && end.After(from) {
			out = append(out, flow.External{Item: item, Span: flow.Span{State: st, Start: start, End: end}})
		}
	}
	byBranch := map[string][]ghRun{}
	for _, r := range s.Runs {
		item := ItemForBranch(r.HeadBranch, items)
		if item == "" {
			continue
		}
		byBranch[r.HeadBranch] = append(byBranch[r.HeadBranch], r)
		start := r.StartedAt
		if start.IsZero() {
			start = r.CreatedAt
		}
		end := horizon // still queued or running
		if r.Status == "completed" {
			end = r.UpdatedAt
		}
		add(item, flow.CIWait, start, end)
	}
	for _, pr := range s.PRs {
		item := ItemForBranch(pr.HeadRefName, items)
		if item == "" || pr.IsDraft || pr.State == "CLOSED" {
			continue
		}
		green, ok := greenSince(byBranch[pr.HeadRefName])
		if !ok {
			continue
		}
		end := horizon // open: still waiting to be merged
		if pr.State == "MERGED" {
			end = pr.MergedAt
		}
		add(item, flow.MergeWait, green, end)
	}
	return out
}

// greenSince is when the newest commit of a branch turned green: all of its
// runs finished, none failed. ok is false while any is going or has failed.
func greenSince(runs []ghRun) (time.Time, bool) {
	var newest time.Time
	var sha string
	for _, r := range runs {
		if r.CreatedAt.After(newest) {
			newest, sha = r.CreatedAt, r.HeadSha
		}
	}
	var green time.Time
	for _, r := range runs {
		if r.HeadSha != sha {
			continue
		}
		if r.Status != "completed" || !passing(r.Conclusion) {
			return time.Time{}, false
		}
		if r.UpdatedAt.After(green) {
			green = r.UpdatedAt
		}
	}
	return green, !green.IsZero()
}

func passing(conclusion string) bool {
	return conclusion == "success" || conclusion == "skipped" || conclusion == "neutral"
}

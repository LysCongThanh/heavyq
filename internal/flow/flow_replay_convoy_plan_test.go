package flow_test

import (
	"fmt"
	"sort"
	"time"
)

// The serial-CI convoy of 2026-11-02, as a synthetic plan. Four pull requests
// are open at once and every one of them touches the same three files: the
// changelog, the usage text and the command table. Main is protected by a
// strict "branch must be up to date" rule, so whenever anything lands, every
// other open pull request merges main again, resolves the same three conflicts,
// and waits for 8 to 10 minutes of CI before it may land itself. The pull
// requests therefore land one after the other, each later one having merged
// main more often than the one before.
//
// The plan is only data: times, branches and what main did. The repo file turns
// it into a real git repository and the gh file into what gh would have said.

var convoyStart = time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)

func minute(n int) time.Time { return convoyStart.Add(time.Duration(n) * time.Minute) }

// The three hotspots, as paths of the repository.
var hotFiles = []string{"CHANGELOG.md", "internal/cli/usage.go", "docs/commands.md"}

type convoyRun struct {
	Branch, SHA string
	Start, End  time.Time
	Cancelled   bool // superseded by the next push before it finished
}

type convoyPR struct {
	ID, Lane, Session string
	Start, Land       time.Time
	Merges            []time.Time // when main was merged into the branch
	Commits           []time.Time // the branch's own commits
	Runs              []convoyRun
	ProofEnds         []time.Time // local gate runs, after each merge
}

func (p convoyPR) Branch() string { return "feat/" + p.ID }

// mainCommit is a commit on main: unrelated work first, then every landing.
type mainCommit struct {
	At    time.Time
	Label string
}

type convoy struct {
	PRs         []*convoyPR
	MainCommits []mainCommit
}

// ciMinutes is the length of the CI run after push number i of PR k: 8 to 10.
func ciMinutes(k, i int) time.Duration { return time.Duration(8+(k+i)%3) * time.Minute }

// planConvoy computes the schedule: each pull request starts five minutes after
// the one before, merges main six minutes (plus two per position in the queue)
// after every commit that lands on main, and lands two minutes after its last
// CI run went green.
func planConvoy() convoy {
	c := convoy{MainCommits: []mainCommit{{minute(30), "m1"}, {minute(55), "m2"}}}
	for k, id := range []string{"statusline-recap", "preflight-spans", "no-hotspots", "flow-foundation"} {
		p := &convoyPR{ID: id, Lane: fmt.Sprintf("agent-%d", k+1), Session: fmt.Sprintf("sess-%d", k+1), Start: minute(5 * k)}
		p.Commits = []time.Time{p.Start.Add(2 * time.Minute), p.Start.Add(25 * time.Minute)}
		push := p.Start.Add(27 * time.Minute)
		p.addRun(k, push)
		mains := append([]mainCommit(nil), c.MainCommits...) // everything that lands before this one does
		sort.Slice(mains, func(i, j int) bool { return mains[i].At.Before(mains[j].At) })
		last := push
		for _, m := range mains {
			at := m.At.Add(time.Duration(6+2*k) * time.Minute)
			if min := last.Add(5 * time.Minute); at.Before(min) {
				at = min
			}
			p.Merges = append(p.Merges, at)
			p.ProofEnds = append(p.ProofEnds, at.Add(5*time.Minute))
			p.addRun(k, at.Add(time.Minute))
			last = at
		}
		green := p.Runs[len(p.Runs)-1].End
		p.Land = green.Add(2 * time.Minute)
		c.MainCommits = append(c.MainCommits, mainCommit{p.Land, "landed-" + id})
		c.PRs = append(c.PRs, p)
	}
	return c
}

// addRun starts a CI run at push; a run still going is cancelled by it.
func (p *convoyPR) addRun(k int, push time.Time) {
	if n := len(p.Runs); n > 0 && p.Runs[n-1].End.After(push) {
		p.Runs[n-1].End, p.Runs[n-1].Cancelled = push, true
	}
	i := len(p.Runs)
	p.Runs = append(p.Runs, convoyRun{Branch: p.Branch(), SHA: fmt.Sprintf("%s-%d", p.ID, i), Start: push, End: push.Add(ciMinutes(k, i))})
}

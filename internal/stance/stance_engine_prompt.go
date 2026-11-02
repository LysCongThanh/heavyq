package stance

import (
	"time"

	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/ledgerindex"
)

// Bounds of the churn read: how many commits, and how far back when the lane
// has no proof to start from. They bound the work, they are not evidence.
const (
	churnMaxCommits = 50
	churnNoProofAge = 24 * time.Hour
)

// Entry is a stance a lane just entered, with the text to inject.
type Entry struct {
	Stance  string
	Trigger Trigger
	Why     string
	// Context is Render(stance, Why): the compact injection.
	Context string
}

// CollectEvidence reads the evidence the stances ask for, for lane at cwd: its
// failure class from the ledger index (one small file) and its churn from git
// (one `git log`), whichever of them some enter rule uses. A source that
// cannot be read is no evidence; the error is returned beside what could be.
func (e Env) CollectEvidence(cwd, lane string, all []Stance) (Evidence, error) {
	var ev Evidence
	wantFailure, wantChurn := NeedsEvidence(all)
	if !wantFailure && !wantChurn {
		return ev, nil
	}
	recs, _, err := ledgerindex.LaneRuns(e.Ledger, lane)
	if wantFailure {
		ev.Failure = SameFailureClassAcrossTrees(recs)
	}
	if wantChurn {
		// The window opens at the lane's last proof: a proof ends churn.
		since := e.now().Add(-churnNoProofAge)
		for i := len(recs) - 1; i >= 0; i-- {
			if counted(recs[i]) && recs[i].Exit == 0 {
				since = recs[i].End
				break
			}
		}
		top, terr := workTreeTop(cwd)
		if terr == nil {
			var commits []gitutil.CommitFiles
			if commits, terr = gitutil.RecentCommits(top, since, churnMaxCommits); terr == nil {
				ev.Churn = ChurnReverts(commits)
			}
		}
		if err == nil {
			err = terr
		}
	}
	return ev, err
}

// OnPrompt is the UserPromptSubmit decision for lane at cwd: it returns the
// stance the lane entered because of this prompt (and the evidence now), or nil
// when nothing changes, which is the normal case. It never injects twice for
// one state: a lane already in the stance, a muted lane, a stance in cooldown
// or at the session cap stay silent.
//
// The user's keyword comes first and is explicit intent. Only when the prompt
// asks for nothing, and the lane is in no stance, is evidence read (ledger
// index and git churn). Stances from an untrusted repository never enter.
func (e Env) OnPrompt(cwd, lane, session, prompt string) (*Entry, error) {
	set, err := e.LoadSet(cwd)
	if err != nil {
		return nil, err
	}
	store, err := set.OpenStoreFor()
	if err != nil {
		return nil, err
	}
	now := e.now()
	st, err := store.Load(lane)
	if err != nil {
		return nil, err
	}
	if st.Muted {
		return nil, nil
	}
	matches := Evaluate(set.Injectable, Evidence{Prompt: prompt})
	if len(matches) == 0 && (st.Active == nil || st.Settle(now) != nil) {
		ev, _ := e.CollectEvidence(cwd, lane, set.Injectable) // partial evidence still counts
		matches = Evaluate(set.Injectable, ev)
	}
	if len(matches) == 0 {
		return nil, nil
	}
	item := LaneItem(set.Common, lane, now)
	var entered *Entry
	err = store.Update(lane, func(st *State) ([]Event, error) {
		var events []Event
		if ex := st.Settle(now); ex != nil {
			events = append(events, *ex)
		}
		for _, m := range matches {
			s, ok := Find(set.Injectable, m.Stance)
			if !ok {
				continue
			}
			evs, skip := st.Enter(EnterRequest{Stance: s, Trigger: m.Trigger, Why: m.Why, Item: item, Session: session}, now)
			if skip == "" {
				entered = &Entry{Stance: s.Name, Trigger: m.Trigger, Why: m.Why, Context: Render(s, m.Why)}
				return append(events, evs...), nil
			}
		}
		return events, nil
	})
	return entered, err
}

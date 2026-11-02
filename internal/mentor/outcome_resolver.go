package mentor

import "github.com/LysCongThanh/heavyq/internal/history"

// OutcomeWindow is how many heavy runs after an event may show a change.
const OutcomeWindow = 3

// ResolveOutcome says what became of event e, given the lane's records
// (oldest first): the primary outcome of the experiment is whether the next
// heavy run differs from the failing one, in run_id (so in argv or tree), within
// OutcomeWindow runs.
//
//   - differed: one of the next OutcomeWindow runs has another run_id;
//   - same: OutcomeWindow runs followed and all repeated the failing run;
//   - pending: fewer followed, all repeating it so far;
//   - unknown: the record that raised the event is not in the list (aged out
//     of the index), so nothing can be told.
//
// A run counts when it ran to its end, a cache replay included: replaying the
// same failure is repeating it. On records without a run_id the tree and the
// command hash stand in for it.
func ResolveOutcome(e Entry, lane []history.Record) string {
	at := -1
	for i, r := range lane {
		if r.ID == e.Event {
			at = i
			break
		}
	}
	if at < 0 {
		return OutcomeUnknown
	}
	seen := 0
	for _, r := range lane[at+1:] {
		if r.Result != "" {
			continue
		}
		if r.RunID != e.RunID || (r.RunID == "" && r.Tree != e.Tree) {
			return OutcomeDiffered
		}
		if seen++; seen == OutcomeWindow {
			return OutcomeSame
		}
	}
	return OutcomePending
}

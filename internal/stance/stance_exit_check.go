package stance

import "time"

// ExitCheck is the verdict on leaving a stance.
type ExitCheck struct {
	// Allowed: the exit condition is met, or the stance has none.
	Allowed bool
	// Reason is the Exit* reason to log when Allowed.
	Reason string
	// Problems says what is missing when not Allowed, one line each.
	Problems []string
}

// CheckExit decides whether the lane may leave its active stance a. The
// stance s says what ends it (exit.decision_record, exit.timebox_minutes);
// record is the decision record of the item, and recordExists says whether the
// file is there at all.
//
//   - a stance without a decision-record condition ends on request;
//   - with one, a record whose structure is complete ends it (structure only:
//     DecisionProblems never grades the thinking);
//   - and so does the time box: once it ran out the stance is over even
//     without a record (Settle does that by itself; CheckExit agrees).
//
// The check is advisory on its own. It binds only where the caller binds it
// (`heavyq stance exit` refuses; a Stop hook does so in steer mode "destroy").
func CheckExit(s Stance, a Active, record []byte, recordExists bool, now time.Time) ExitCheck {
	if !s.Exit.DecisionRecord {
		return ExitCheck{Allowed: true, Reason: ExitManual}
	}
	problems := []string{"no decision record yet"}
	if recordExists {
		problems = DecisionProblems(string(record))
		if len(problems) == 0 {
			return ExitCheck{Allowed: true, Reason: ExitDecisionRecord}
		}
	}
	if !a.Expires.IsZero() && !now.Before(a.Expires) {
		return ExitCheck{Allowed: true, Reason: ExitTimebox}
	}
	return ExitCheck{Problems: problems}
}

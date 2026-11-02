// Package preflight decides, before a long unattended run, whether it can
// succeed without a human: a goal and items exist, every item has a runnable
// proof, nothing waits on an answer, the machine has room, no permission
// prompt would block the agent, and the quota is not nearly spent.
//
// Every check is a pure function of facts the caller gathered (internal/cli
// reads the anchor, the ledger, the settings, the machines), so each one is a
// table test. The checks return Findings; Build turns them into a Report with
// a GO or NO-GO verdict. A finding of severity Block flips the verdict, one of
// severity Warn is listed but does not. Every finding carries the command (or
// edit) that fixes it, so the human can act on the line without thinking.
package preflight

// Names of the checks, the "check" field of a finding.
const (
	NameAnchor       = "anchor"
	NameDependencies = "dependencies"
	NameEnvironment  = "environment"
	NamePermissions  = "permissions"
	NameQuota        = "quota"
)

// Severity of a finding.
type Severity string

const (
	// Block: a human must handle it now, the verdict is NO-GO.
	Block Severity = "block"
	// Warn: worth knowing, the run can still go.
	Warn Severity = "warn"
)

// Verdicts.
const (
	Go   = "GO"
	NoGo = "NO-GO"
)

// ReportSchema is the version of the JSON document of Report.
const ReportSchema = 1

// Finding is one question or fix for the human.
type Finding struct {
	Check    string   `json:"check"`
	Severity Severity `json:"severity"`
	Subject  string   `json:"subject,omitempty"` // the item, machine or command it is about
	Text     string   `json:"text"`
	Fix      string   `json:"fix"` // the command or edit that resolves it
}

// Report is the outcome of a preflight.
type Report struct {
	Schema   int       `json:"schema"`
	Verdict  string    `json:"verdict"`
	Goal     string    `json:"goal"`
	Hours    float64   `json:"hours"` // 0 when the run has no stated length
	Parallel int       `json:"parallel"`
	Items    []string  `json:"items"`   // the items the run will work on
	Checked  []string  `json:"checked"` // checks that ran
	Skipped  []string  `json:"skipped"` // checks that could not run, with the reason
	Findings []Finding `json:"findings"`
}

// Blocks counts the findings that make the verdict NO-GO.
func (r Report) Blocks() int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == Block {
			n++
		}
	}
	return n
}

// Results is what one check hands to Build.
type Results struct {
	Name    string    // the check
	Skipped string    // why it did not run; "" when it did
	Found   []Finding // empty when all is well
}

// Plan is the run being checked.
type Plan struct {
	Goal     string
	Hours    float64
	Parallel int
	Items    []string
}

// Build assembles the report: blocks first, then warnings, each group in the
// order the checks ran. The verdict is NO-GO when any finding blocks.
func Build(p Plan, rs []Results) Report {
	r := Report{Schema: ReportSchema, Goal: p.Goal, Hours: p.Hours, Parallel: p.Parallel,
		Items: nonNil(p.Items), Checked: []string{}, Skipped: []string{}, Findings: []Finding{}}
	for _, sev := range []Severity{Block, Warn} {
		for _, c := range rs {
			for _, f := range c.Found {
				if f.Severity == sev {
					f.Check = c.Name
					r.Findings = append(r.Findings, f)
				}
			}
		}
	}
	for _, c := range rs {
		if c.Skipped != "" {
			r.Skipped = append(r.Skipped, c.Name+": "+c.Skipped)
		} else {
			r.Checked = append(r.Checked, c.Name)
		}
	}
	r.Verdict = Go
	if r.Blocks() > 0 {
		r.Verdict = NoGo
	}
	return r
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

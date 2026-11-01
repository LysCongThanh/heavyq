package history

import "fmt"

// Loaded says whether the machine was over a limit of the resource gate when
// r was admitted: the run was let in by the anti-starvation bypass, the
// 1-minute load was at or above the limit, or less RAM was free than the job's
// budget. It is false for a record that carries no machine reading (older
// lines, runs replayed from the cache, machines that cannot be measured):
// without a measurement nothing is claimed.
func (r Record) Loaded() bool { return r.LoadWhy() != "" }

// LoadWhy is Loaded with the reason in words ("" when not loaded).
func (r Record) LoadWhy() string {
	switch {
	case r.Forced:
		return "admitted past a resource limit by the anti-starvation bypass"
	case r.Cores == 0:
		return ""
	case r.MaxLoad > 0 && r.Load1 >= r.MaxLoad:
		return fmt.Sprintf("load %.2f >= %.2f at start", r.Load1, r.MaxLoad)
	case r.JobMB > 0 && r.FreeMB < r.JobMB:
		return fmt.Sprintf("%d MB free < %d MB job budget at start", r.FreeMB, r.JobMB)
	}
	return ""
}

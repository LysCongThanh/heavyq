package agentmode

import (
	"fmt"
	"regexp"
	"time"
)

// logNameRe is the shape of every log heavyq saves. Pruning touches nothing
// else, so a logs directory shared with other tools is safe.
var logNameRe = regexp.MustCompile(`^\d{8}T\d{6}\.\d{6}Z-\d+-[^/]*\.log$`)

// LogName is the file name (with ".log") of the log of a run that started at
// t, as process pid, in lane: 20261102T010203.123456Z-4242-wt-auth.log. Names
// sort by time, and two runs of one process never collide.
func LogName(t time.Time, pid int, lane string) string {
	return fmt.Sprintf("%s-%d-%s.log", t.UTC().Format("20060102T150405.000000Z"), pid, lane)
}

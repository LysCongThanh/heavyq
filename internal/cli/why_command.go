package cli

import (
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/LysCongThanh/heavyq/internal/agentmode"
	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/queue"
	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

// whyInsightsWindow is the history window the story's last sentence covers.
const whyInsightsWindow = time.Hour

// whyCommand answers "why is it like this?" in words. Without a lane it
// prints the story the dashboard shows (and `status --json` carries); with a
// lane, that lane's running and waiting jobs, the reason per machine, and its
// last recorded run. A lane that is not in the queue is not an error: the
// answer is that it is idle.
func whyCommand(cfg *config.Config, args []string, w, stderr io.Writer) int {
	fs := flag.NewFlagSet("why", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, "usage: heavyq [--config FILE] why [lane]") }
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() > 1 || (fs.NArg() == 1 && fs.Arg(0) == "") {
		fs.Usage()
		return ExitUsage
	}
	s, err := buildSnapshot(cfg, newProbeCache(cfg.Queue.ProbeTimeout.Duration), cpuSampleInterval)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	// History only adds colour. A broken log must not hide the live answer,
	// but it is reported, not swallowed.
	recs, err := history.ReadLast(history.DefaultPath(), 0)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq: history unreadable, showing live state only:", err)
	}
	now := time.Now()
	var lines []string
	if fs.NArg() == 1 {
		lane := queue.SanitizeLane(fs.Arg(0)) // the name runs were filed under
		lines = snapshot.LaneLines(s, lane, lastRunOfLane(recs, lane), now)
	} else {
		ins := snapshot.ComputeInsights(recs, now.Add(-whyInsightsWindow), now, 1)
		for _, sent := range snapshot.Story(s, &ins, now) {
			lines = append(lines, sent.Text)
		}
		if l := gateWhyLine(recs, stderr); l != "" {
			lines = append(lines, l)
		}
	}
	if outputMode(stderr).Terse() { // agent mode: the answer fits in ~150 tokens
		lines = agentmode.CapLines(lines, agentmode.WhyMaxBytes, "... (heavyq status shows the rest)")
	}
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
	return 0
}

// lastRunOfLane returns the newest record of lane (records are oldest
// first), or nil.
func lastRunOfLane(recs []history.Record, lane string) *history.Record {
	for i := len(recs) - 1; i >= 0; i-- {
		if recs[i].Lane == lane {
			return &recs[i]
		}
	}
	return nil
}

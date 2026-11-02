package collect

import (
	"errors"
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/flow"
)

func TestItemForBranch(t *testing.T) {
	ids := []string{"flow", "flow-collectors", "cli-race-fast", "x"}
	for branch, want := range map[string]string{
		"feat/flow-collectors":       "flow-collectors", // the longest id wins over "flow"
		"fix/flow-collectors-2":      "flow-collectors",
		"flow-collectors":            "flow-collectors",
		"Feat/Flow-Collectors":       "flow-collectors",
		"feat/flow":                  "flow",
		"feat/cli-race-fast-retry":   "cli-race-fast",
		"feat/flowers":               "", // "flow" inside a longer word is not the item
		"feat/prefixflow-collectors": "",
		"worktree-agent-aff0b62d":    "",
		"main":                       "",
		"feat/x":                     "x",
	} {
		if got := ItemForBranch(branch, ids); got != want {
			t.Errorf("ItemForBranch(%q) = %q, want %q", branch, got, want)
		}
	}
	if got := ItemForBranch("feat/a", nil); got != "" {
		t.Errorf("no items: %q", got)
	}
}

func TestAllRunsEveryCollectorAndJoinsTheirErrors(t *testing.T) {
	boom := errors.New("gh down")
	g := &fakeGH{err: boom}
	o := Options{Items: []string{"login"}, GH: g.run, Now: func() time.Time { return at(0) }}
	ext, err := All(o)(t.TempDir(), at(-60), at(0)) // not a repository: git has nothing either
	if len(ext) != 0 || !errors.Is(err, boom) {
		t.Errorf("%v %v", ext, err)
	}
	o.NoGH = true
	g.calls = 0
	if _, err := All(o)(t.TempDir(), at(-60), at(0)); err != nil || g.calls != 0 {
		t.Errorf("NoGH must not touch gh: %v, %d calls", err, g.calls)
	}
	var _ flow.Collector = All(o)
}

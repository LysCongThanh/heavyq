package collect

import (
	"testing"
	"time"

	"github.com/LysCongThanh/heavyq/internal/flow"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
)

func TestConflictSpanIsTheTimeSinceThePreviousCommitWithinBounds(t *testing.T) {
	for name, tc := range map[string]struct {
		prev time.Time
		want time.Duration
	}{
		"eight minutes since the last commit": {at(12), 8 * time.Minute},
		"idle for hours is capped":            {at(-300), ConflictCap},
		"seconds after the last commit":       {at(20).Add(-5 * time.Second), minConflict},
		"a clock running backwards":           {at(30), minConflict},
		"unknown":                             {time.Time{}, ConflictCap},
	} {
		sp := conflictSpan(gitutil.MergeCommit{At: at(20), PrevAt: tc.prev})
		if sp.State != flow.ConflictRework || !sp.End.Equal(at(20)) || sp.Duration() != tc.want {
			t.Errorf("%s: %+v, want %v ending at the merge", name, sp, tc.want)
		}
	}
}

func TestGitCollectorWithoutItemsOrRepositoryYieldsNothing(t *testing.T) {
	if ext, err := Git(Options{})(t.TempDir(), at(-60), at(0)); ext != nil || err != nil {
		t.Errorf("no items: %v %v", ext, err)
	}
	if ext, err := Git(Options{Items: []string{"login"}})(t.TempDir(), at(-60), at(0)); len(ext) != 0 || err != nil {
		t.Errorf("not a repository: %v %v", ext, err)
	}
}

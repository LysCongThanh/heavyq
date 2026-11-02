package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/deadline"
	"github.com/LysCongThanh/heavyq/internal/flow"
	"github.com/LysCongThanh/heavyq/internal/flowstate"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
)

// flowOffEnv turns the automatic flow refresh off for a process (hooks and the
// end of a run). `heavyq flow refresh` still works. The test suite sets it for
// runs inside a repository it does not own.
const flowOffEnv = "HEAVYQ_FLOW"

// flowAutoBudget bounds an automatic refresh at the end of a run or in a hook:
// past it the refresh is abandoned and the next event tries again.
const flowAutoBudget = 4 * time.Second

// errNotRepo: the directory is outside any git repository, so there is no
// place to keep flow state.
var errNotRepo = errors.New("not inside a git repository")

func flowDisabled() bool { return os.Getenv(flowOffEnv) == "off" }

// refreshFlow writes the flow state of the repository around dir when it is due
// (force: now). wrote says whether it did. cfg is loaded lazily, only when
// there is something to write.
func refreshFlow(loadCfg func() (*config.Config, error), dir string, force bool) (wrote bool, err error) {
	common, err := gitutil.CommonDir(dir)
	if err != nil {
		return false, fmt.Errorf("%w: %s", errNotRepo, dir)
	}
	store := flowstate.Store{Common: common}
	return store.Refresh(force, func() (flow.Snapshot, error) {
		cfg, err := loadCfg()
		if err != nil {
			return flow.Snapshot{}, err
		}
		in, _, err := gatherFlow(cfg, dir, time.Now())
		if err != nil {
			return flow.Snapshot{}, err
		}
		return flow.Build(in), nil
	})
}

// autoRefreshFlow is the refresh of the natural events (the end of a run, a
// hook): fail open, bounded, and silent. Whatever goes wrong goes to hook.log.
// Outside a git repository there is nothing to do.
func autoRefreshFlow(who string, loadCfg func() (*config.Config, error), dir string, force bool) {
	if flowDisabled() || dir == "" {
		return
	}
	_, err := deadline.RunWithin(flowAutoBudget, func(context.Context) (bool, error) {
		return refreshFlow(loadCfg, dir, force)
	})
	if err != nil && !errors.Is(err, errNotRepo) {
		hookLog("%s: flow refresh: %v", who, err)
	}
}

// defaultConfigLoader loads the config the way Main does when no --config was
// typed: for the places (hooks, the end of a run) that have no cfg at hand.
func defaultConfigLoader() (*config.Config, error) {
	return config.Load(config.DefaultPath(), os.Getenv("HEAVYQ_CONFIG") != "")
}

// flowRefreshesOn are the hook events whose handlers (brief, stop) also bring the
// flow state up to date, debounced. The flow hook handles its own events.
var flowRefreshesOn = map[string]bool{"SessionStart": true, "Stop": true}

// refreshFlowAfterHook is called by the hook runner once a reply is out.
func refreshFlowAfterHook(name, event, cwd string) {
	if flowRefreshesOn[event] && cwd != "" {
		autoRefreshFlow(name, defaultConfigLoader, cwd, false)
	}
}

// refreshFlowAfterRun is the end-of-run refresh, debounced like the others. The
// work tree is the one the run was started in.
func refreshFlowAfterRun(cfg *config.Config) {
	wd, err := os.Getwd()
	if err != nil {
		return
	}
	autoRefreshFlow("run", func() (*config.Config, error) { return cfg, nil }, wd, false)
}

package cli

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/remote"
	"github.com/LysCongThanh/heavyq/internal/toolcheck"
)

// toolCache is where the answers of the tool probes are kept: beside the ledger
// ($XDG_STATE_HOME/heavyq/remote-tools, or beside $HEAVYQ_HISTORY). Without a
// state directory nothing is kept and every placement asks again.
func toolCache() toolcheck.Cache {
	p := history.DefaultPath()
	if p == "" {
		return toolcheck.Cache{}
	}
	return toolcheck.Cache{Dir: filepath.Join(filepath.Dir(p), "remote-tools")}
}

// toolProbeTimeout bounds one tool probe: the metrics probe's timeout plus the
// 10 s the job script gives a macOS login shell to print its PATH.
func toolProbeTimeout(cfg *config.Config) time.Duration {
	return cfg.Queue.ProbeTimeout.Duration + 10*time.Second
}

// remoteToolAnswers says, for each remote of names, which of tools resolve on
// the PATH a job gets there: the cached answers, and a probe (all remotes in
// parallel) for the rest. With fresh, nothing cached is used. mayProbe, when
// not nil, says which remotes may be asked now (the run asks only those its
// metrics probe reached); the others answer from the cache alone. A probe also
// asks for toolcheck.Common, and what it learns is cached. A tool with no answer
// is unknown; errs holds why a remote could not be asked.
func remoteToolAnswers(cfg *config.Config, names, tools []string, mayProbe func(string) bool, fresh bool) (map[string]map[string]toolcheck.Answer, map[string]error) {
	cache := toolCache()
	answers := map[string]map[string]toolcheck.Answer{}
	errs := map[string]error{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, n := range names {
		m, ok := cfg.Machine(n)
		if !ok || m.IsLocal() {
			continue
		}
		key := toolcheck.Key(m.EnvPath, m.Transport)
		known, stale := cache.Lookup(n, key, tools, time.Now())
		if fresh {
			known, stale = map[string]toolcheck.Answer{}, tools
		}
		answers[n] = known
		if len(stale) == 0 || (mayProbe != nil && !mayProbe(n)) {
			continue
		}
		wg.Add(1)
		go func(m config.MachineConfig, known map[string]toolcheck.Answer, stale []string) { // known is this remote's alone
			defer wg.Done()
			paths, err := remote.ProbeTools(m, toolcheck.Union(stale, toolcheck.Common), toolProbeTimeout(cfg))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs[m.Name] = err
				return
			}
			got := toolcheck.Answers(paths, time.Now())
			_ = cache.Store(m.Name, key, got) // a cache that cannot be written costs a probe next time
			for _, t := range stale {
				if a, ok := got[t]; ok {
					known[t] = a
				}
			}
		}(m, known, stale)
	}
	wg.Wait()
	return answers, errs
}

// missingTools lists, per remote, the tools it answered it does not have
// (unknown tools are not missing), and only remotes with any.
func missingTools(answers map[string]map[string]toolcheck.Answer, tools []string) map[string][]string {
	out := map[string][]string{}
	for n, a := range answers {
		for _, t := range tools {
			if ans, ok := a[t]; ok && !ans.Found {
				out[n] = append(out[n], t)
			}
		}
	}
	return out
}

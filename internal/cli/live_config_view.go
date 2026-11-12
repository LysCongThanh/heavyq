package cli

import (
	"sync"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// liveConfig is the config of a view that stays open (the dashboard, `heavyq
// top`): it follows edits of the file like a waiting ticket does, so the view
// explains each ticket with the config the ticket decides with, not the one
// the view started with. An edit that does not load leaves the last good
// config (the view has nowhere to say why; `heavyq status` names the error).
// Safe for concurrent use: the dashboard builds snapshots from HTTP handlers.
type liveConfig struct {
	mu    sync.Mutex
	cur   *config.Config
	watch *config.Watch
}

func newLiveConfig(cfg *config.Config) *liveConfig {
	return &liveConfig{cur: cfg, watch: config.NewWatch(cfg)}
}

// get is the config as the file holds it now (one stat while it is unchanged).
func (l *liveConfig) get() *config.Config {
	l.mu.Lock()
	defer l.mu.Unlock()
	if fresh, err := l.watch.Changed(l.cur); err == nil && fresh != nil {
		l.cur = fresh
	}
	return l.cur
}

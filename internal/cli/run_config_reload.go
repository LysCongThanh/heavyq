package cli

// reloadConfig adopts an edit of the config file made while the ticket waits.
// There is no daemon: the waiting process decides its own admission on every
// poll, while `heavyq status` and `why` are fresh processes that read the file
// as it is now. A waiter that kept the config it started with would refuse a
// slot or a load limit the operator just added, and status would promise it a
// start it never makes (and every ticket behind it would wait in turn).
//
// What the ticket is stays as it was submitted: its target, candidate machines
// and needs, and the cache settings its cache key and run identity were made
// with. Everything admission reads (queue timing, slots, limits, the remotes
// among its candidates, pools, the probe timeout) comes from the new file. An
// edit that does not load is reported once and changes nothing: the ticket
// keeps its last good config (fail closed). So does an edit whose pools cannot
// be read because the repository's heavyq.toml does not load; that one is
// retried on every poll, since fixing heavyq.toml does not touch the config
// file. It returns a non-zero exit code when the ticket must give up.
func (r *run) reloadConfig() int {
	fresh, err := r.cfgWatch.Changed(r.cfg)
	if err != nil {
		r.note("config edit not applied while waiting: %v; keeping the last good config", err)
		return 0
	}
	if fresh == nil {
		return 0
	}
	next := *fresh
	next.Cache = r.cfg.Cache
	if r.poolSet != nil {
		set, err := poolSetFor(&next, r.needsDir())
		if err != nil {
			r.cfgWatch.Retry()
			if msg := err.Error(); msg != r.poolRefusal {
				r.poolRefusal = msg
				r.note("config edit not applied while waiting: %v; keeping the last good config", err)
			}
			return 0
		}
		r.poolRefusal = ""
		if err := set.Validate(r.opts.Needs); err != nil {
			r.giveUp("the config changed while waiting and --needs no longer fits: %v; giving up", err)
			return ExitUsage
		}
		r.poolSet = set
	}
	r.cfg = &next
	r.probes.setTimeout(next.Queue.ProbeTimeout.Duration)
	r.note("config changed while waiting; this ticket now follows it")
	return 0
}

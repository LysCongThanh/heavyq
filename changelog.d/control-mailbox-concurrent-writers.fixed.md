**Two cancel or priority requests for one ticket at the same moment no longer fail or vanish.** The dashboard and
`heavyq cancel|reprio|pause` wrote every request through one shared temporary file, so a second click, a second
tab or the CLI racing the dashboard could get an error for a request that was stored, or store an empty priority
that the job then dropped. Each writer now has a temporary file of its own. A job picking up a priority change now
claims the request before reading it, so a newer change that arrives meanwhile waits for the next poll instead of
being deleted unseen.

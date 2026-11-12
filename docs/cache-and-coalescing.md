# Never run the same gate twice (cache + coalescing)

![four agents run the same npm test on the same commit: one runs, three wait for it and reuse its result, and a later run is a cache hit](coalesce.gif)

```bash
heavyq 4 --cache -- npm test
```

- **Key.** sha256 of the run id (the HEAD tree, the exact argv, the cwd relative to the repo root, the values
  of the variables listed in `cache.env` and the base ref commit, if any (`--base-ref`, `queue.base_ref`, else
  origin's default branch; `--base-ref none` for none: a `git fetch` that moves it is a miss); the same id the ledger records
  as `run_id`) and the heavyq version. A new commit
  with the same tree (an empty commit, a rebase that changes nothing) still hits.
- **Only clean trees.** If anything tracked is modified, or any untracked file isn't ignored, the run
  goes ahead normally and nothing is cached. `-v` says why, and history records `skip` with the
  reason. The tree is checked again after the run. If it changed meanwhile, the result is not stored.
- **Coalescing.** If an identical key is running right now, the second caller doesn't queue a duplicate
  ticket. It waits for that run and replays its output and exit code. `status` shows
  `3 waiting on lane api's run`. This works for failures too: the callers that waited get the same
  failure. If the first run is killed or never starts, one of the waiting callers runs the command
  itself.
- **Hits.** Output is replayed to stdout and stderr in the order it was captured, then the exit code is
  returned, followed by `heavyq: cached result from 2026-11-01 20:51:18 (local)` on stderr. Successes are
  cached. Failures are cached only with `cache_failures = true`.
- **Storage.** `~/.cache/heavyq/results` (`$XDG_CACHE_HOME`), one output file plus metadata per key,
  written atomically under a `flock`. Expired entries (`ttl`, default 24h) are dropped, then the oldest go
  until the total fits `max_size_mb` (512). `heavyq cache ls`, `heavyq cache stats` (with hit, coalesced and
  miss counts from history) and `heavyq cache clear` manage it.

**Honest limits:**

- The key sees committed content only. Ignored files (`.env.test`, `node_modules`, a local database), the
  toolchain version, the OS and the network are invisible to it. If they change the result, list the
  relevant variables in `cache.env`, or don't use `--cache` for that command.
- A command that writes untracked, non-ignored files into the repo (coverage reports, build output) makes
  the tree dirty, so its result is never stored. Ignore those paths in `.gitignore`.
- With `--cache`, the command's stdout and stderr are pipes, not your terminal, so tools may turn colours
  off. The order of output between stdout and stderr is kept as heavyq received it, not more precisely.
- Results are per machine and per user. Entries aren't shared between machines.
- Not with `--ref` other than HEAD (skipped, with the reason). With `--with`, the pinned commit of each sibling is
  part of the key, and a local result is not stored if a sibling changed while the command ran.

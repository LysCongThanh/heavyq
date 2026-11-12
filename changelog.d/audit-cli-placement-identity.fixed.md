- **A remote run gets the `cache.env` values it is recorded with.** Those variables are part of the run id and the
  cache key, but a remote job ran with the remote's own environment, so a remote pass could prove (and be replayed
  for) an environment that never ran. The remote command now gets exactly the local values, and loses the unset ones.
- **The ledger names the base a remote run really got.** With `--base-ref`, the run id used the local ref as it was
  when the run ended (a fetch during the run changed it) and kept the base even when it could not be pushed, and
  `--cache` stored such a no-base result under the base it never got. Jobs of one repository on a remote also no
  longer move each other's base ref: each job checks its commit out in a repository of its own (borrowing the
  objects of the shared bare one), so jobs with different bases run side by side.
- **Remote runs use the right subdirectory.** `--repo` naming a subdirectory, run from elsewhere, ran at the
  repository root remotely; the subdirectory now comes from git, which also fixes a `cd` typed with another case on
  macOS. Default placement keeps a run local in a directory a remote cannot use (unusual characters, or not in the
  commit, like an ignored `build/`) instead of failing it, and a named remote refuses it up front.
- **Items and the stop gate see `with` gates.** An item proved by a gate with `with` siblings could never be done,
  and the steer facts kept calling such a gate unproven, because they left the sibling commits out of the run id.
- **A dirty `with` sibling proves nothing.** While a sibling pinned to its `HEAD` has uncommitted edits, the gate is
  not proven (a local run would read the edits, which nothing ran); `gate status` says to commit them.
- **A waiting ticket keeps its ticket file up to date.** When a `--with` sibling or `--ref` changed which machines a
  waiting ticket can use, only its memory knew: other waiters blocked on an idle machine or jumped ahead of it.
- **A cancel or a signal during a scheduling round stops the ticket before its command starts.** It used to start the
  command and stop it a second later.
- **One queue for every launch context.** Without `XDG_RUNTIME_DIR` (cron, `env -i`, services) heavyq used
  `/tmp/heavyq-<uid>`, a second queue with its own slots, next to the login session's. It now uses
  `/run/user/<uid>` when that directory is the user's, and on macOS the per-user temp directory even without
  `TMPDIR`.
- **A clock stepped back no longer holds RAM for an hour.** The RAM held for just-started jobs ignores claims that
  seem to lie in the future.

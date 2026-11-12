- **Tree identity no longer trusts files git is told to skip.** A file flagged `assume-unchanged` or
  `skip-worktree`, and every path outside a sparse checkout's cone, is reported unchanged by `git status` and
  `git diff`, so a run on edited or missing files was recorded and cached as a pass for `HEAD`'s full tree. Such
  a tree is now unknown: it proves nothing and is never cached. The same goes for such files inside a
  checked-out submodule, at any depth. Flags on heavyq's own bookkeeping files still do not count.
- **A submodule that is not checked out no longer shares the id of one that is.** Every `git worktree add`
  (so every agent work tree) leaves submodules empty, yet got the same tree id as the main checkout, so a pass
  there proved, and with `--cache` replayed into, a checkout whose submodule files the command never saw. Such
  a tree is now dirty with an id of its own, also when the missing submodule sits inside a checked-out one;
  `git submodule update --init --recursive` restores the clean id.

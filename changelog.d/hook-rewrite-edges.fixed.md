- **Hook: a sub-second `hook.max_wait` no longer means "wait forever".** It rounded to `--max-wait 0s`, which heavyq
  reads as no limit. The config now refuses a value under `1s`, and the hook rounds up to whole seconds.
- **Hook: the Bash tool timeout follows Claude Code's own limits.** The hook read neither `BASH_DEFAULT_TIMEOUT_MS`
  nor `BASH_MAX_TIMEOUT_MS`, so with a raised default it lowered the timeout, and over the cap it wrote a value
  Claude Code cut back silently. For a foreground call it now never writes less than Claude Code's default or
  more than its cap, and tells the agent when queue time comes out of the command's own time. A
  `run_in_background` call keeps Claude Code's background limits: its own `timeout` plus the queue wait, or
  no `timeout` at all when it set none.
- **Hook: assignments that would change heavyq itself are left alone.** `PATH=... go test` failed with 127, and
  `HOME=`, `TMPDIR=`, `XDG_*=` or `HEAVYQ_*=` in front of a command sent heavyq to another queue, config or
  ledger. Such commands now run unwrapped, as do commands whose program PATH can't find (an alias or a shell
  function, which failed with 127 under heavyq). The reason is in `hook.log`.
- **Hook: a `cache = true` rule no longer caches `VAR=x` variants under the plain command's key.**
  `GOFLAGS=-race go test ./...` replayed the cached non-race pass. `--cache` is now dropped unless every
  assignment is listed in `cache.env`.
- **`heavyq hook install` and `steer` keep a symlinked `settings.json`.** They replaced the link with a regular
  file, so a dotfiles checkout never saw the change. The link's target is now updated, and a dangling link is
  refused.

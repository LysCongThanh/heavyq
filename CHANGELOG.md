# Changelog

All notable changes to heavyq. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- **Ledger v2: every run is bound to the code it ran on.** History lines gain `v` (schema, `2`), `id` (a
  ULID that sorts by time), `repo` (hash of the git common dir, shared by all work trees), `worktree`,
  `subdir`, `commit`, `tree`, `dirty`, `session`, `item` and `gate`. Lines written by older versions (no
  `v`) keep loading. Remote runs record the commit that was shipped, its tree, and `dirty: false`. Outside
  a git repository the identity fields stay empty.
- **Tree identity for dirty work trees.** A clean tree is identified by `HEAD`'s tree id, exactly what the
  `--cache` key already used. A dirty one is `<HEAD tree>+<digest>`: the digest covers `git diff HEAD --binary`
  (staged or not) and the names and contents of untracked files that aren't ignored. The same edits give the
  same id, any edit changes it, reverting restores the clean id. About 20 ms clean and 35 ms dirty on a 10,000-file repository
  (`go test ./internal/gitutil -run '^$' -bench TreeIdentity`). Dirty trees are still never cached.
- **Run flags `--session`, `--item` and `--gate`**, plus `HEAVYQ_SESSION` and `HEAVYQ_ITEM`, recorded in the
  ledger. Names are 1-80 characters of `A-Za-z0-9._:-`; anything else exits 2.
- **The Claude Code hook records the session out of band.** When it wraps a command it writes
  `<queue dir>/sessions/<lane>` (`{session_id, at}`, atomic, 0600). `heavyq <prio>` uses it when `--session`
  and `HEAVYQ_SESSION` are absent and the file is under 10 minutes old with a valid id. The rewritten command
  is unchanged, so exact permission rules keep matching across sessions. Stale files are pruned with dead
  tickets.
- **`heavyq history`** shows the short commit of each run (`+` when the work tree was dirty); `-v` adds the
  tree, session, item and gate.
- **`heavyq.toml`** at the git root: per-repository named gates (`[[gate]]` with `name`, `run`, `prio`,
  `light`, `remote`) and a reserved `[anchor]` section. The loader (`internal/repoconfig`) validates names,
  non-empty `run` lists, `prio` 1-9 and remote names, and refuses unknown keys (with the line) and
  duplicate gate names. It is trusted like a Makefile, and hooks never execute gates. No command uses it
  yet.
- **`heavyq why [lane]`**: the story of the queue in plain words ("2 agents wait because every heavy slot
  on local is taken."). With a lane, its running and waiting jobs, the reason per candidate machine, and its
  last recorded run.
- **`snapshot.story`** in `status --json` and the dashboard stream: the same sentences, as plain `text` plus
  `parts` with emphasis. Additive within schema 1. `heavyq status` now opens with them under `STORY`.

### Changed

- **Monthly history files.** Without `HEAVYQ_HISTORY`, runs are appended to
  `$XDG_STATE_HOME/heavyq/history-YYYY-MM.jsonl` (UTC). `heavyq history`, `heavyq cache stats` and the
  dashboard read across the months, newest last, and still include the `history.jsonl` written by older
  versions. `HEAVYQ_HISTORY` pointing at a file still means exactly that file, never rotated.
- A run records its `commit` but no `tree` when the tree is not certain: a local command edited its own work
  tree, the tree changed between a cached or coalesced result's key and its replay (the record used to get the
  tree of replay time), a submodule or untracked nested repository has changes of its own, more than 256 MiB
  of untracked content would have to be read, or `HEAD` moved during identification. An unknown tree can never
  count as proof.
- Tree identity details: `HEAD` is resolved once per identification and used in every git call; edits that
  cancel out (staged, then reverted) give the clean id; `git diff` is pinned against user config
  (`diff.algorithm`, `diff.indentHeuristic`, `diff.submodule`, `core.quotePath`, context, prefixes); untracked
  files over 64 MiB are identified by size, mtime, inode and change time.
- `git status` runs with `--ignore-submodules=none`, so `submodule.<name>.ignore` and `diff.ignoreSubmodules`
  can no longer make a changed submodule look clean (this also tightens the `--cache` key).
- Ledger ids are ULIDs that order by creation time at millisecond resolution only; order records by file
  position or `end`.
- heavyq's read-only git queries run with `GIT_OPTIONAL_LOCKS=0`, so they never make a concurrent `git add`
  or `git commit` fail with "index.lock exists".
- The dashboard's opening sentences are computed in Go and rendered from `snapshot.story`. The page keeps its
  own copy of the logic as a fallback for older servers and the demo data. The sentences are unchanged apart
  from two grammar fixes ("1 node process holds", and a lane that got in through the bypass is named once).

## [0.3.0] - 2026-11-01

### Added

- **`heavyq dash`**: a local web dashboard. One embedded page, no daemon, no new dependencies. It shows
  machines (RAM vs what a job needs, load vs limit, slots), running jobs with CPU and memory, waiting jobs with
  the reason per machine and the anti-starvation countdown, identical runs sharing one result, what runs
  *outside* the queue by executable, and insights from history (wait p50/p95, give-ups, forced admissions,
  cache hits, busiest lanes).
  - Loopback only, a random token per start, `Host` and `Origin` checks, a strict CSP. Read-only unless
    started with `--control`.
- **`heavyq status --json`**: the same state as a versioned JSON snapshot (schema 1).
- **Operator control**: `heavyq cancel <pid>`, `heavyq reprio <pid> <1-9>`, `heavyq pause|resume <machine>`
  (also from the dashboard). Requests go through `<queue dir>/ctl`, and each job's own process applies them.
  A cancelled job exits 78. A running one gets SIGTERM, its whole process tree included, then SIGKILL 10 s
  later.
- History records more: jobs that gave up or were cancelled (`result`), admissions through the
  anti-starvation bypass (`forced`), and CPU time and peak memory of local runs (`cpu_s`, `max_rss_mb`).
- Likely secrets in command lines (`TOKEN=…`, `scheme://user:pass@`, `--password …`, well-known token
  shapes) are hidden in `status --json` and the dashboard.

## [0.2.0] - 2026-11-01

### Added

- **Claude Code hook.** `heavyq hook claude-pretooluse` is a `PreToolUse` hook for the Bash tool. It
  rewrites heavy commands (`npm test`, `go test ./...`, `pytest`, `cargo test`, ...) into heavyq runs through
  `hookSpecificOutput.updatedInput`, so agents don't need to know heavyq exists.
  - Configurable `[[hook.rule]]`s: priority, `light`, fixed `lane`, `cache`. Built-in rules can be turned
    off with `default_rules = false`.
  - The lane is the git work tree's name, unless a rule or `$HEAVYQ_LANE` sets it.
  - Accepts only shapes it can parse safely: `[cd DIR &&] [VAR=x ...] CMD [2>&1] [| tail|head|grep]`.
    Leaves everything else unchanged, as well as commands already wrapped and calls made while
    `HEAVYQ_ACTIVE` is set.
  - Adds `--max-wait` and raises the Bash tool `timeout` by the same amount.
  - Fails open: on any error it prints nothing and exits 0, and the error goes to `hook.log`.
  - Approves nothing by default; `hook.permission = "allow"` opts in.
- `heavyq hook install claude [--scope local|project|user] [--print]` and `heavyq hook uninstall claude`.
  They merge into Claude Code settings, write a `.bak` first, are idempotent, and refuse malformed files.
- **Result cache and coalescing (`--cache`).** Results are keyed by the HEAD tree, the exact argv, the
  subdirectory, the variables in `cache.env`, the `--base-ref` commit and the heavyq version. Only clean
  work trees are cached, and the tree is checked again after the run.
  - Identical runs that are in progress are coalesced: the second caller waits for the first and replays
    its output and exit code.
  - Finished results are replayed (stdout and stderr in captured order) with a one-line stderr note.
  - Failures are not cached unless `cache_failures = true`.
  - Storage under `$XDG_CACHE_HOME/heavyq/results`, with a TTL and a size cap, atomic writes and a `flock`.
- `heavyq cache ls|stats|clear` (`--json` for `ls` and `stats`).
- `status` shows runs waiting on an identical run (`3 waiting on lane api's run`).
- History records `cache` (`hit`, `coalesced`, `miss`, `skip`) with a note, and `heavyq history` shows it.
- `-v` / `--verbose` explains cache decisions.
- Commands run by heavyq get `HEAVYQ_ACTIVE=1`.
- `docs/claude-code.md`: the hook behaviour heavyq relies on, quoted from the Claude Code docs, plus a
  dogfood run with Claude Code 2.1.316.

## [0.1.1] - 2026-10-31

### Fixed

- `heavyq version` reports the module version when installed with `go install`.

## [0.1.0] - 2026-10-31

### Added

- Priority tickets, heavy and light slots, RAM and load gate with anti-starvation, `status` with per-lane
  CPU attribution, remote runs by exact commit over SSH with score-based placement, and JSONL history.

[0.3.0]: https://github.com/LysCongThanh/heavyq/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/LysCongThanh/heavyq/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/LysCongThanh/heavyq/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/LysCongThanh/heavyq/releases/tag/v0.1.0

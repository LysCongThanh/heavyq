# Changelog

All notable changes to heavyq. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/).

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

[0.2.0]: https://github.com/LysCongThanh/heavyq/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/LysCongThanh/heavyq/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/LysCongThanh/heavyq/releases/tag/v0.1.0

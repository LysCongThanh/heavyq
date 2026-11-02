# Changelog

All notable changes to heavyq. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- **Named pools: ports, databases and browsers handed out to parallel agents** (`internal/pools`,
  [docs/pools.md](docs/pools.md)). `[[pool]]` tables (`name`, `values` as a range `"4100-4199"` or a list, or
  `size`) in the user config and in `heavyq.toml` (the repository's pool replaces the user's of the same name).
  `heavyq 4 --needs port:2,pg,browser -- npm run e2e` is admitted only when its slot and every need can be taken
  in one scheduling round, so it never holds part of what it asked for. Each value is a `flock` file held by the
  run's heavyq process, released when it ends or dies. The command gets `HEAVYQ_PORT_1`, `HEAVYQ_PORT_2`,
  `HEAVYQ_PG`, `HEAVYQ_BROWSER`. Port pools skip ports that are bound now (listen probe on `127.0.0.1` and `::1`)
  and the OS ephemeral range. Gates may declare `needs = ["pg", "port:2"]`. A ticket with needs runs locally
  (a named `--remote` is refused). `status` has a `POOLS` section and `status --json` a `pools` array (and `needs` /
  `pools` per ticket); `why` says `waiting for pg: 3/3 held by a, b, c`; the Tower's starved card and the
  dashboard's machines section show pools. The ledger gains the flat fields `needs` and `pools`. A ticket waiting
  for a pool does not hold back a later ticket that shares none of its pools.

### Added

- **Ledger tail reader and indexes: hooks stay under 20 ms.** A full scan of a 100,000-line ledger (about
  55 MB, a month of a busy fleet) took 0.3-0.7 s. `history.ReadTail` now reads the newest records backwards
  in chunks, across the monthly files, with a record count and/or a `since` time (the last 2,000 records of
  that ledger in about 5 ms); corrupt and oversized lines are skipped as before. Beside the ledger,
  small JSON files are updated under a lock at the end of every run: per lane the last 50 records
  (`index/lanes/`), per `(repo, tree)` the conclusive runs with the latest outcome and pass/fail counts per
  `run_id` (`index/trees/`), both bounded (the least recently touched are evicted). They are derived data:
  a missing, stale or damaged index is detected (the ledger size is recorded) and readers fall back to a
  bounded tail read, never to a full scan. `gate status`, `why`, the Tower (red gates, loops, starved lanes),
  `history`, and the dashboard's insights windows use them. **`heavyq doctor [--reindex]`** is new: it
  prints the index state and size, and rebuilds it from the ledger. The same ledger lines also decode about
  4x faster (a hand-written decoder with `encoding/json` as the fallback, fuzzed against it). See
  `docs/ledger.md`.
- **`internal/deadline`: `RunWithin(d, fn)`**, the fail-open time budget for the hooks to come (SessionStart,
  Stop, PostToolUse): a handler that misses its deadline or panics yields an error and the hook says nothing.
  A test holds the hook path through the index under 20 ms on a 100,000-record ledger.
- **Claude Code plugin marketplace.** The repository is its own marketplace (`.claude-plugin/marketplace.json`,
  name `heavyq`) with a `plugins/heavyq` plugin: `/plugin marketplace add LysCongThanh/heavyq`, then
  `/plugin install heavyq@heavyq`. It carries the `PreToolUse` Bash hook, wrapped as
  `sh -c 'command -v heavyq >/dev/null 2>&1 && exec heavyq hook claude-pretooluse || exit 0'` so a missing
  binary is silent, and a `heavyq` skill (gate status/run, why, `help agents`, exit codes 75/78, never bypass).
  The binary is not bundled. Go tests check the plugin files (structure, required fields, hook command).
- `heavyq hook install claude` refuses (exit 2, with a `next:` line) when the user, project or local settings
  enable `heavyq@heavyq`, because the plugin already runs the same hook and two rewriting hooks have no defined
  winner. `--force` installs anyway; `--print` and `uninstall` are never refused.
### Fixed

- **The tree id ignores heavyq's own bookkeeping files.** `heavyq item done` ticks a box in the anchor file, which
  changed the tree id, so the proof behind `done` read as stale at once (and committing the tick changed it
  again). The anchor file (`[anchor] file`, default `.heavyq/anchor.md`), `.heavyq/decisions/` and
  `.heavyq/stances/` are now left out of the identity: the edits' diff and untracked listing skip them, and
  the committed part is `HEAD`'s tree with those entries removed (only the root and `.heavyq/` levels are read
  and re-hashed, nothing is written to the repository). `heavyq.toml` and any other file still count. **One-time
  cache miss:** the run identity schema (`run_id`) and the cache key schema are bumped, and dirty ids use a new
  digest prefix, so proofs and cached results from older versions miss once and gates run again. See
  `docs/gates.md`, "What the tree id ignores".

### Changed

- **README is a short hero** (tagline, a 14-second proof GIF recorded from real runs, install, Run / Prove / Steer).
  The reference sections moved into `docs/` (`queue.md`, `remote.md`, `configuration.md`, `exit-codes.md`, ...) unchanged.
  `docs/tapes/proof.tape` records the GIF; the `demo` workflow re-records it on release tags.

## [0.4.0] - 2026-11-02

### Added

- **macOS parity: RAM/load gate, CPU attribution, process-tree cancel** (no cgo). Local metrics come from
  `sysctl -n hw.memsize hw.ncpu vm.loadavg` and `vm_stat` (available = free + inactive + purgeable +
  speculative pages, capped at installed RAM), so `heavyq status --json` has `metrics_known: true` on a Mac.
  Processes come from `ps -axo pid=,ppid=,rss=,time=,comm=` (CPU time parsed from `[[dd-]hh:]mm:ss[.cc]`,
  executable paths with spaces kept whole), sampled twice for CPU% per lane and for the outside-the-queue
  groups. A cancel walks the `ps` ppid map and reaches every grandchild still attached to its parent;
  orphans re-parented to `launchd` can be missed (macOS has no subreaper). Linux keeps `/proc`, unchanged.
  If a command fails, the metrics stay unknown and the gate only enforces slots. CI gains a `macos` job.
- **Stances: thinking modes as files** (`internal/stance`, `heavyq stance ls|show <name> [--json]`). A stance
  is a short markdown document describing a way of thinking, with TOML frontmatter between `+++` lines
  (`name`, `description`, `enter` triggers, `exit` conditions, `cooldown_minutes`, `inject_tokens`; unknown
  keys are errors with the line; body up to 32 KiB, any language). Five built-ins ship embedded:
  `breakthrough`, `converge`, `skeptic`, `calm`, `surgeon`. Precedence: repo `.heavyq/stances/*.md` over
  user `$XDG_CONFIG_HOME/heavyq/stances` (or `~/.config/heavyq/stances`) over built-in, same name overrides,
  and an invalid file is an error, never a silent fallback. A compact renderer produces the injection text,
  facts first (`[heavyq · stance <name>] why now: ...`, `next: ...`, at most 3 core lines from a
  `<!-- core -->` block, `full: heavyq stance show <name>`), within `inject_tokens` (default 150, max 400,
  about 4 bytes per token). `enter.stall` stays parseable but is status-line-only: no built-in enters on
  elapsed time alone. This is the pure building block: triggers and injection
  come next, nothing changes an agent's behaviour yet. See `docs/stances.md`.
- **Dashboard Tower: an exception-first home.** The top of the page is one calm line (`Nothing needs you. 4
  lanes working, 2 unproven.`) unless a fact heavyq measured needs a human: a **red gate** (the latest
  conclusive gate run on a lane's current tree failed), a **loop** (the same command failed twice on the same
  tree within 30 minutes), a **starved** lane (head of the queue past 2/3 of `force_after`, or a give-up in
  the last hour) or **machine** trouble (unreachable, paused, over 40% of RAM outside the queue). Each card
  has the evidence and at most one action (a button with `--control`, else the command to copy) and can be
  dismissed for 10 minutes in that browser. `status --json` gains `exceptions[]` and `gates[]` (additive,
  schema 1) and `heavyq why` lists each exception in one `Needs you:` line. See `docs/dashboard.md`.
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
  duplicate gate names. It is trusted like a Makefile, and hooks never execute gates.
- **`heavyq gate status [name] [--json]`**: is this exact code proven? Per gate command, `proven`, `failed`
  or `missing` on the tree checked out now (clean or dirty), with when, lane and run time, read from the
  ledger without running anything (about 20 ms). The latest conclusive run on the tree decides, unless the
  command is flaky (below); gave-up, cancelled and interrupted runs are inconclusive; a cache hit counts. An unidentifiable tree (a submodule
  with edits of its own, no commit, not git) proves nothing and says why. Exit 0 proven, 1 not, 2 usage
  error. `--json` is documented in `docs/gates.md`.
- **`heavyq gate run [name] [--parallel] [--keep-going] [--lane NAME]`**: runs the gate's unproven commands
  through the queue exactly like `heavyq <prio> --cache --gate <name> --remote <r> -- sh -c '<command>'`,
  with the gate's `prio`, `light` and `remote`. Proven commands are skipped at once; it stops at the first
  failure unless `--keep-going`. Exit 0 only when the ledger proves the whole gate on the code checked out
  when it ends.
- **`internal/proof`**: the matching rules behind both (same repo, same tree, the exact `sh -c` command,
  exit 0 and a finished run), pure and table-tested.
- **Flaky and under-load detection (#30).** The ledger records the machine as it was when a run was admitted:
  `load1`, `free_mb`, `cores`, plus the limits applied to it (`max_load`, `job_mb`); local runs use the sample
  the resource gate judged, remote runs the probe placement used. `heavyq gate status` (text and `--json`)
  classifies every failed command: **`flaky`** when on this tree the command both failed and passed. It is
  a label, and proof recovers by repetition: proven once its last two runs passed ("proven (flaky: 1 fail /
  2 pass on this tree)"), not proven while the latest run failed or only one pass follows the failure
  ("flaky — re-run once to confirm: heavyq gate run ready"; `gate run` then really re-runs it, without
  `--cache`), **`under_load`** when the
  failing run was admitted by the anti-starvation bypass, at or above the load limit, or with less free RAM
  than its job budget ("rerun once before debugging"), else **`failed`**. A failure under load does not make
  a command flaky and never breaks a repetition, so a clean rerun proves it. `--json` gains `commands[].class`, `passes` and `fails`, and
  `reason` now also carries the evidence. `heavyq why` shows the counts in its gate sentence
  ("1 failed (1 flaky), 0 missing"). Flaky and under-load failures never raise the Tower's `red_gate`
  exception. Lines without the new fields load as before and are never called under
  load.
- **`cmd_hash` in the ledger (#38).** Each line carries the sha256 of the exact argv the run executed, taken
  before the command is redacted and cut to 200 bytes. Proofs match on it when the record has one, so a gate
  command longer than 200 bytes can now be proven; records without it (written by older versions) still
  match by their command string, and a long command they cannot identify stays `missing` until it runs again.
- **heavyq proves itself.** This repository has a `heavyq.toml` with gate `ready` (gofmt, `go vet`,
  `go test -race`), and CI runs `heavyq gate run ready` with the freshly built binary instead of separate
  vet and test steps.
- **`heavyq why`** ends with "The ready gate is proven / not proven on this tree" when the repository has a
  `heavyq.toml`.
- **Zero-config default gate.** A repository with no `heavyq.toml` gets a gate called `ready`, detected at its
  git root: `go.mod` gives `go vet ./...` and `go test ./...`; `package.json` gives `<pm> run <script>` for the
  `typecheck`, `lint` and `test` scripts it defines (`<pm>` from the lockfile: `pnpm-lock.yaml`, `yarn.lock`,
  `bun.lockb`/`bun.lock`, else npm; the `echo "no test specified"` stub of `npm init` does not count);
  `pytest.ini`, `pyproject.toml` or a `[tool:pytest]` section in `setup.cfg` gives `pytest`; `Cargo.toml` gives
  `cargo test`. Several ecosystems at the root add up, in that order. `gate status`, `gate run` and the `why`
  line use it and call it `ready (detected)`; `gate status --json` adds `"detected": true`. Detection only reads
  files (a bounded read, `package.json` parsed as data, script bodies never used) and never runs anything. An
  explicit `heavyq.toml` always wins.
- **`heavyq init [--agents-md] [--force] [--print]`** writes that detection down as a commented `heavyq.toml`
  at the git root: what the file is, the trust model, how to add a gate. It never overwrites an existing file
  (exit 1) unless `--force`; `--print` writes to stdout only. `--agents-md` adds a short section ("before
  saying a task is done, run `heavyq gate run ready`; exit 1: fix and re-run; exit 75: queue busy, retry
  later, never bypass heavyq") to `AGENTS.md` (created if missing) and to `CLAUDE.md` (only if it exists),
  between `<!-- heavyq:begin -->` and `<!-- heavyq:end -->`. Running it again changes nothing, a stale section is
  replaced in place, and a symlink that leaves the repository is never followed. Safe for an agent to run.
- **`heavyq why [lane]`**: the story of the queue in plain words ("2 agents wait because every heavy slot
  on local is taken."). With a lane, its running and waiting jobs, the reason per candidate machine, and its
  last recorded run.
- **`snapshot.story`** in `status --json` and the dashboard stream: the same sentences, as plain `text` plus
  `parts` with emphasis. Additive within schema 1. `heavyq status` now opens with them under `STORY`.
- **Agent mode: terse output, a result digest, next-step lines** ([docs/agent-mode.md](docs/agent-mode.md)).
  It turns on when the environment says a coding agent is calling (`CLAUDECODE`, `AI_AGENT`, `GEMINI_CLI`,
  `COPILOT_CLI`, and best-effort `CODEX_SANDBOX`, `CODEX_CI`, `CODEX_THREAD_ID`, `CURSOR_AGENT`), or with
  `HEAVYQ_OUTPUT=agent` / `HEAVYQ_AGENT=1`; `HEAVYQ_OUTPUT=human` / `HEAVYQ_AGENT=0` force it off. Nothing
  changes for people. In agent mode:
  - a run prints at most three lines of its own: `heavyq: queued #N of M ...` (only after 2 s of waiting,
    then a heartbeat every 5 minutes), `heavyq: running <cmd> (lane, slot, tree)` (the first line of the
    output, for agents that keep only the head and tail of a failing command's output), and the digest;
  - the **digest** ends the output (stderr, under 4 KB, no ANSI): `heavyq: PASS|FAIL|REUSED|NOT RUN|CANCELLED|INTERRUPTED
    <cmd> · tree <short> · <time>`; on a failure up to 12 failure-looking lines taken from the last 64 KiB of
    the command's output (the stream itself is not changed); for a cache replay "already passed on this exact
    tree at <time> (lane X); do not re-run"; and, for every verdict but a pass, exactly one `next:` line
    (75: `retry later; don't bypass heavyq`, 78: `cancelled by operator; ask before re-running`, remote down,
    an unproven gate: `heavyq gate run <name>`);
  - the full output of a run that did not succeed is saved (first 4 MiB, mode 0600) in `logs/` beside the
    ledger and its path is in the digest; the newest 30 are kept, none older than 7 days;
  - `heavyq why` is cut to 600 bytes, `gate status` / `gate run` end with `next:`, and usage errors say
    `next: fix the arguments`.
- **`--digest`** prints the result digest for a person too (the command's stdout and stderr then go through
  pipes, so a tool that colours only on a terminal stops colouring). `HEAVYQ_OUTPUT=json` ends with the
  digest as one JSON line.
- **`heavyq help agents`**: a usage card of at most 900 bytes (about 200 tokens). `heavyq help <unknown>`
  now exits 2 instead of printing the usage.
- Golden tests enforce the budgets: a 2-minute wait is one queued line (three with the running line and the
  digest), a digest under 4 KB with 6 MB of failing output, `why` at most 600 bytes, the card at most 900.
- **`internal/agentmode`**: detection, the output capture (bounded tail and log file), failure-line
  extraction, the digest and the card. The end-to-end rig removes the agent variables from the child
  environment, so the suite behaves the same when it runs under a coding agent.

### Changed

- **`gate run --parallel` runs each command as its own child process.** heavyq starts itself once per
  command (`heavyq <prio> --cache --gate ... -- sh -c '<command>'`, same `--config`) instead of running the
  commands in goroutines of one process. Every ticket now has a pid of its own, so `heavyq cancel <pid>` and
  `reprio <pid>` hit exactly one command; each command has its own signal handling and no shared stdin. The
  output of each child is streamed line by line with a `[cmd N]` prefix (N is the command's number in the
  gate, as in `[N/total]`; stdout stays on stdout, stderr on stderr). A SIGINT, SIGTERM or SIGHUP sent to
  `gate run` is relayed to every child, and exit codes are aggregated as before (an operator cancel is 78, a
  signal 128+n, anything else is judged by the ledger). Sequential `gate run` is unchanged.
- `heavyq cancel|reprio <pid>` refuses a pid that has more than one ticket (it names their ids) instead of
  picking one; a ticket id always works.
- **Cache key schema 2.** The key is now derived from the run id (below) plus the heavyq version. Results
  cached by earlier versions miss once and are rebuilt. A `--base-ref` that does not resolve now counts as no
  base ref (it used to be a different key).
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

### Fixed

- **A run in another environment could prove a gate it never ran (#46).** "The same run" had two
  definitions: the cache keyed on tree, argv, env allowlist and base ref, while proof matched the redacted
  command string. So `FEATURE_X=1 heavyq gate run` made `heavyq gate status` call the gate proven for a shell
  without `FEATURE_X`. There is now one definition, the **run id** (`internal/runident`): the sha256 of the
  tree id, the exact argv, the subdirectory, the values of the `cache.env` variables (set, empty and unset
  differ) and the `--base-ref` commit. Local or remote is not part of it. Every ledger line carries it as
  `run_id` (`cmd_hash` stays and is a component of it). Proof matches on `run_id` when the record and the gate
  have one, and flaky detection (a pass and a failure of the same run) and the Tower's loop detection use it
  too, so the same command under another environment is not a retry or a flake. A record without `run_id`
  (written by an older heavyq) is matched by `cmd_hash`, or by the command string on the oldest lines, only
  when `cache.env` is empty; with an allowlist such a record cannot say which values it ran with, so it does
  not prove and `gate status` says to run it again. See [docs/gates.md](docs/gates.md).
- **The user config (`~/.config/heavyq/config.toml`) keys are case-sensitive too.** Same cause as for
  `heavyq.toml`: the TOML library matched keys ignoring case, so `[LOCAL]` or `Heavy_Slots = 3` loaded
  silently, and `[LOCAL]` next to `[local]` merged into one table, with an undefined winner. Any key with an
  upper-case letter is now refused as an unknown key, with the file and line. Unknown keys in this file now
  name their line as well. The check lives in the new `internal/tomlkeys`, shared with `heavyq.toml`; a new
  fuzz target covers the config parser.
- **`heavyq.toml` keys are case-sensitive.** The TOML library matched keys to fields ignoring case, so
  `[[GATE]]` or `Run = [...]` loaded silently, and `[[gaTe]]` next to `[[gate]]` filled the same list: one
  of the two gates was dropped, and which one was random. Any key with an upper-case letter is now refused
  as an unknown key, with its line. Found by the new fuzz tests.
- **A ledger line over 1 MiB no longer stops `heavyq history`.** It is skipped like any corrupt line, and the
  records after it still load (before, the read ended with an error at that line).

### Tests

- Go fuzz tests for the parsers that read untrusted input: the hook's shell-command parser and rewrite,
  secret redaction, the anchor markdown parser and `SetChecked`, `heavyq.toml`, the user config, and the ledger reader. CI
  runs each for 20 s on pull requests that touch its package and for 2 minutes on the weekly schedule.

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

[Unreleased]: https://github.com/LysCongThanh/heavyq/compare/v0.4.0...HEAD
[0.4.0]: https://github.com/LysCongThanh/heavyq/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/LysCongThanh/heavyq/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/LysCongThanh/heavyq/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/LysCongThanh/heavyq/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/LysCongThanh/heavyq/releases/tag/v0.1.0

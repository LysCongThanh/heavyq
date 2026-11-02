# Changelog

All notable changes to heavyq. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- **Signed releases.** Every archive and `checksums.txt` now has a cosign keyless signature
  (`<file>.sigstore.json`, made by the release workflow through Sigstore with GitHub OIDC), next to the existing
  SPDX SBOMs and GitHub build-provenance attestation. This is what the Scorecard Signed-Releases check reads. The
  release footer and [docs/install.md](docs/install.md#verify-a-release) show `cosign verify-blob` and
  `gh attestation verify`. Releases are immutable (repository setting), and `v*` tags are protected from deletion
  and force-push (ruleset).
- **`install.sh`**, attached to every release and listed in its signed `checksums.txt`: detects OS and CPU,
  downloads the archive, **checks its SHA-256 against `checksums.txt`** (a mismatch or a duplicated line installs
  nothing), installs into `~/.local/bin` or `--prefix DIR/bin` atomically, and with `--verify` also checks the
  cosign signature. `--version` pins a release. Tested in CI on Linux and macOS against a local fake release and
  against real releases ([docs/install.md](docs/install.md)).
- **`heavyq <command> --help`** (and `heavyq help <command>`) prints a real usage block: what the command does,
  its synopsis, its flags with descriptions, and a link to its docs, instead of the Go flag defaults. It works
  without a config and for subcommands (`heavyq gate run --help`). A `--help` after `--` still belongs to your
  command.
- **`heavyq completion bash|zsh|fish`**: shell completion scripts for commands, subcommands, flags and their
  values (`--remote local|auto`, `--scope ...`, paths); after `--` they complete the program you queue. They run
  on bash 3.2 (macOS) and need no bash-completion package.
- **Man page** `heavyq(1)`, generated at release by `tools/mangen` and shipped in every archive as
  `man/man1/heavyq.1`. The help blocks, completions and man page are generated from one command table
  (`internal/cli/command_table_*.go`); tests fail when it and the code or the usage text disagree.
- **JSON Schemas** in `schema/` for `heavyq.toml`, the user config and `heavyq status --json`, generated from the
  Go types by `tools/schemagen` (a separate module, so the main module keeps its single dependency). Add
  `#:schema https://raw.githubusercontent.com/LysCongThanh/heavyq/main/schema/heavyq.schema.json` as the first line
  of a file for completion, hover help and errors in Taplo-based editors
  ([docs/configuration.md](docs/configuration.md#editor-support-json-schemas)). CI regenerates them and fails on a
  difference.
- **Steer: a brief after compaction and a stop gate that wants proof** ([docs/steer.md](docs/steer.md),
  `internal/steer`).
  - **`heavyq brief [--json]`**: the goal, the asking lane's items, the one next command, a red gate, items that need
    a human and one machine-pressure line, in at most 300 tokens (bytes / 4; a test holds the worst case). The
    SessionStart hook injects it as `additionalContext` on startup, resume and compact, so `/compact` cannot erase
    the thread. Silent in a repository that never used heavyq.
  - **`heavyq steer off|guard|destroy`**, stored per repository in the git common dir (default `off`;
    `HEAVYQ_STEER` overrides it, `off` is the kill switch). The **Stop hook** blocks while the lane holds claimed
    items that are not proven on this tree, and every block names the exact next command
    (`heavyq item done <id> --run`) and an escape (`item block ... "needs: ..."`, `item release`). `guard` blocks
    at most 3 times per session and honours `stop_hook_active` (no second block on an unchanged situation); items
    that are blocked, released, without a proof, or whose proof was **already red when the session started**
    (a baseline recorded at SessionStart) never block. `destroy` has no cap, also gates SubagentStop,
    TaskCompleted and TeammateIdle (exit 2 with the reason on stderr) and repeats the brief in every block;
    `heavyq steer destroy` prints how to lift Claude Code's native cap (`CLAUDE_CODE_STOP_HOOK_BLOCK_CAP=0`) and
    `--apply` writes it to the Claude Code settings `env` block (a `.bak` is kept, idempotent).
  - **Claim vs ledger**: when the last assistant message says the tests pass (two conservative phrases) and the
    ledger shows a plain failure on this tree that was not red at the start, the Stop hook continues once with the
    fact, without blocking.
  - **Delivery canary**: each brief carries a short random token; `heavyq doctor` looks for it in the transcript
    path the hook was given (best effort, never changes the exit code).
  - **`heavyq hook install claude --features queue,brief,stop`** (default `queue`; the exact set, foreign hooks are
    kept) and the plugin's `hooks.json` now carry SessionStart, Stop, SubagentStop, TaskCompleted and
    TeammateIdle, behind the same `command -v heavyq` guard. New hooks plug in with one row in the feature table
    and the shared hook input/output helpers (`internal/hook/claude_hook_io.go`). Hook handlers run under
    `deadline.RunWithin` and fail open. Golden tests pin each hook's JSON; table tests cover the stop decisions;
    an end-to-end test drives a temporary repository (claim, edit, Stop blocks with the next command, gate run,
    Stop allowed).

- **`heavyq top`: a terminal monitor for the pane next to your agents** ([docs/top.md](docs/top.md)). It reads
  the same snapshot as `status --json` and the dashboard and redraws every 2 s (`--interval`): what needs you
  first (red gates bound to their tree, loops, starved jobs, machines down, blocked items), one row per ticket
  with the short reason it waits, machines with a RAM bar marked at what a heavy job needs, pools and the story
  sentence. Five display modes, `--mode full|focus|lanes|machines|line` or `m` at runtime (the last one is
  remembered in `$XDG_STATE_HOME/heavyq/top-mode`); `--line` / `--once` print one frame for tmux
  `status-right` or scripts. Keys: `j k g G Tab`, `Enter` (why), `/` filter, `r`, `?`, `y` (copy, OSC 52),
  `q`; with `--control`, `c` (inline y/n), `+ -` (priority) and `p` (pause) go through the same control mailbox
  and checks as the dashboard. No TUI framework: hand-rolled ANSI, alternate screen restored on exit,
  SIGINT/TERM/HUP, Ctrl-Z/SIGCONT and panics, synchronized output, width measured in cells, `NO_COLOR`, ASCII
  fallback, a 60-column floor. The one new dependency is `golang.org/x/term` (raw mode and terminal size).
  Humans only: the `help agents` card is unchanged.

- **macOS remotes: run heavy jobs on a spare Mac** ([docs/remote.md](docs/remote.md#a-spare-mac-as-a-remote)).
  A stock Mac (no `flock`, no `setsid`, no Homebrew) is now a valid `[[remote]]`. Where util-linux is missing the
  remote script uses Perl, which ships with macOS: `flock()` for the slot lock (the same `flock(2)` on the same
  lock files, so busy slots look the same to the probe) and `POSIX::setsid()` for the command's process group.
  The stdin lifeline teardown is unchanged and was checked against a real Mac for cancel, heavyq dying and a
  hard disconnect, with no process left behind. The probe reads `sysctl` and `vm_stat` through the same parser
  and formulas as the local macOS sampler (`metrics.ParseDarwin`). Remote commands get the login shell's PATH on
  macOS, and the new per-remote **`env_path`** key puts extra directories first (`~/` is the remote home), for
  toolchains the login shell does not set, such as `~/.local/go-sdk/bin`. Linux remotes behave as before.

- **`heavyq receipts [--day|--week|--month] [--share] [--json]`: a shareable, privacy-rounded summary**
  (`internal/receipts`, [docs/receipts.md](docs/receipts.md)). Counts from the ledger window: heavy runs,
  duplicate runs avoided (cache hits plus coalesced), runs held back in the queue, flaky and under-load
  failures flagged, remote runs and machines, pool values handed out, wait p50/p95 and agents (lanes) served.
  Each metric counts only what the ledger proves; "freezes avoided" is left out because admission records
  cannot show it. `--share` rounds into ranges ("~40 runs/day", "about 1 in 8 runs"), drops every lane,
  repository, command, path and machine name, adds the heavyq version and the window, and prints a compact
  box-drawing card of at most 12 lines. The dashboard gets a "Receipts" panel (week/month toggle, "Copy to
  share") fed by the new token-protected `GET /api/v1/receipts?window=day|week|month`.
- **Mentor: break repeat failures, measured with a holdout** (`internal/mentor`, [docs/mentor.md](docs/mentor.md)).
  The same `run_id` (same tree, argv and env) failing again with a deterministic exit code gets one short note,
  built from ledger fields only: `[heavyq · tool data] <fact>.` at the 2nd failure, with `Next: <one action>.` at
  the 3rd. Never for 124, 137, 130, 143 or heavyq's own 75-78 and 255, never for `under_load` or flaky runs. It
  speaks on a state change only: each level once per signature, 3 events per rule and session, a 3 s cooldown.
  **`heavyq hook claude-posttooluse`** (for both `PostToolUse` and `PostToolUseFailure`, which is what Claude
  Code sends for a failed Bash command) reads the lane's newest records from the ledger index, fails open under
  the 15 ms hook budget, and prints `additionalContext` only for the inject arm. Every eligible event is logged
  to `mentor/interventions.jsonl` with its rule, seed, arm (a seeded 80/20 draw), event and outcome (did the next
  heavy run differ, by `run_id`, within 3 runs). **`heavyq mentor stats [--json]`** shows events, resolved,
  differed and rate per rule and arm, and the lift in points; `mentor mute|unmute <rule>`. A rule under 10 points
  of lift after 200 resolved events per arm mutes itself. New `[mentor]` config (`holdout_pct`, `auto_mute`,
  `min_events`, `min_lift_pp`; `holdout_pct = 100` is shadow mode), `HEAVYQ_MENTOR=off`, and
  `ledgerindex.LaneRuns` (a lane's newest records from the index). The settings entry is documented; `hook
  install` does not write it yet.
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
- **`heavyq item ls|claim|release|done|block`: claim, prove and finish work items.** It joins the anchor
  (`.heavyq/anchor.md` plus read-only `[anchor] extra` sources), the lease store and the run ledger. Status is a
  pure function of those facts (`internal/itemstatus`): `open`, `claimed` (lane, expires), `proven` (short
  commit, time), `stale` (proven on an older tree), `blocked` (needs) and `claimed_done_unproven` (a box ticked
  with no proof). `proof: gate:<name>` uses the gate's status and `proof: <cmd>` the run identity of `sh -c
  <cmd>`, like a gate command; a claimed item is judged on its lane's work tree. Lane comes from
  `$HEAVYQ_LANE` or the work tree name, session from `$HEAVYQ_SESSION` or the hook's session file. Leases
  are TTL-based, and **any heavyq run in a lane renews that lane's leases** when it ends (fail-open, no event).
  `claim --next` never hands one item to two worktrees. `done` refuses without proof (exit 1, `next: heavyq
  gate run <name>` or `heavyq item done <id> --run`); `--run` runs the proof through the queue, tagged with the
  item, and on success ticks exactly that checkbox under the lease lock (plan-file items are only marked done
  in the events log, never rewritten) and releases the lease. `block <id> "needs: ..."` records the need and
  frees the item until it is claimed again. All commands take `--json` where they print data and end with a
  `next:` line in agent mode. `status --json` gains `items[]` (additive, schema 1) and the Tower a `blocked`
  exception. The lease package gains `Finish`, `RenewLane`, `Events` and `WithLock`. See `docs/items.md`.

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

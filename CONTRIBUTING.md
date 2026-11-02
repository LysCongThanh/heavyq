# Contributing to heavyq

Thanks for helping. heavyq is small on purpose. One binary, no daemon, and the queue is a directory.
Contributions that keep it that way are the easiest to merge.

## Getting started

```bash
git clone https://github.com/LysCongThanh/heavyq && cd heavyq
go test -race ./...        # unit + end-to-end tests (real processes, real flock, an sh -c "remote")
go vet ./... && gofmt -l .
heavyq gate run ready      # the full proof CI runs on main (gofmt, vet, race tests), if heavyq is installed
golangci-lint run          # v2, config in .golangci.yml (CI pins the version)
```

You need Go (see `go.mod`), `git`, `bash` and util-linux (`flock`, `setsid`). No ssh server is required.
The remote path is tested through a `sh -c` transport, which runs the same code ssh would.

## Where things live

| Package | Responsibility |
|---|---|
| `internal/cli` | flags, the wait loop, execution, `status`, `history`, `cache`, `hook` commands |
| `internal/hook` | Claude Code hooks: the `PreToolUse` safe shell-form parser and wrap rules; the input and output objects the other hooks share (`claude_hook_io.go`); the feature table and settings install/uninstall (`claude_features.go`) |
| `internal/agentmode` | agent mode: detection (`AgentEnv` table), bounded output capture and log, failure lines, the result digest, the `help agents` card |
| `internal/cache` | `--cache`: key by git tree, output recorder/replay, result store (TTL, size cap), in-flight coalescing |
| `internal/schedule` | pure rules: who may claim which machine, the resource gate, outside-repo path detection |
| `internal/queue` | tickets as files, ordering, claim, prune |
| `internal/slots` | `flock(2)` slot files (compatible with `flock <file> cmd`) |
| `internal/procinfo` | process identity and per-lane CPU attribution from `/proc` (Linux) or `ps` (macOS) |
| `internal/metrics` | RAM / load / cores from `/proc` or `sysctl` + `vm_stat` (macOS), placement score |
| `internal/remote` | transport, probe, `git push` of exact commits, the embedded remote job script |
| `internal/config` | TOML config, defaults, validation |
| `internal/history` | the run ledger: JSONL records (commit, tree, session, ...), ULIDs, monthly rotation; `ReadTail` reads the newest records backwards, `Each`/`EachFrom` stream; `historytest` makes synthetic ledgers for tests |
| `internal/ledgerindex` | rebuildable indexes beside the ledger (per lane, per tree), written under a lock where a record is appended (`Append`); hot-path reads (`TreeRuns`, `LastOfLane`, `Recent`) with a bounded-tail fallback; `heavyq doctor --reindex`. See `docs/ledger.md` |
| `internal/deadline` | `RunWithin(d, fn)`: fail-open time budget for hook handlers |
| `internal/gitutil` | git queries, including the tree identity of clean and dirty work trees |
| `internal/runident` | the `run_id`: the one definition of "the same run" (tree, argv, subdir, `cache.env` values, base ref) shared by cache, ledger, proof, flaky and loop detection |
| `internal/proof` | pure matching of ledger records to gate commands on a tree (by `run_id`, else `cmd_hash`): proven / failed (flaky, under load) / missing |
| `internal/mentor` | the mentor: repeat-failure detector over lane records, the note renderer, per-lane state (cap, cooldown), the seeded 80/20 holdout, the intervention log, outcome resolution, stats and auto-mute. See `docs/mentor.md` |
| `internal/anchor` | the anchor checklist: lenient reader, extra read-only sources, one-checkbox rewrite |
| `internal/lease` | per-item leases and the events log under the repository's common dir, with a directory flock |
| `internal/itemstatus` | pure derivation of an item's status from anchor line, lease, events and proofs |
| `internal/steer` | the brief (`Brief`, at most 300 tokens) and the stop gate (`DecideStop`, a pure function of facts and the session record), the claim-vs-ledger phrase matcher, the steering mode and per-session records under the repository's common dir, the delivery canary. See `docs/steer.md` |
| `internal/flow` | pure derivation of how work moves: per-item (else per-session) timelines of states, fleet aggregates, throughput, the constraint, and the `Snapshot` that is `flow/state.json`. See `docs/flow.md` |
| `internal/flowstate` | the I/O around `flow`: `state.json` written atomically, debounced and under `flock`; the hook events log; the per-session block flags |
| `internal/sessionmeta` | the per-session record (model, agent version, rate limits) kept from the status-line JSON; the ledger joins `agent` and `model` on it |
| `internal/repoconfig` | per-repo `heavyq.toml`: named gates, validation (trusted like a Makefile) |
| `internal/tomlkeys` | strict-key check shared by both config parsers: unknown or upper-case keys, with their line |
| `internal/cli/*_command_spec.go` | one file per command: its table entry (name, synopsis, flags, subcommands) and its dispatch, registered from `init()` through `command_registry.go`. The table generates `heavyq --help`, `<cmd> --help`, `heavyq completion` and the man page, and `Main` dispatches through it; drift tests tie it to the flag definitions |
| `tools/changelog` | folds `changelog.d/` fragments into `CHANGELOG.md` at release time; `check` is the PR rule CI applies |
| `tools/mangen` | writes the man page from that table (run by goreleaser at release) |
| `tools/schemagen` | separate module (own `go.mod`): writes `schema/*.json` from the config and snapshot types |
| `schema/` | generated JSON Schemas for `heavyq.toml`, the user config and `status --json`; do not edit by hand |
| `scripts/` | `install.sh` (a release asset) and its test, `test-install.sh` |

## House rules

- **Stdlib first.** The only dependency is `BurntSushi/toml`. Ask in an issue before adding another.
- **Small files.** Keep each file under ~200 lines, with `snake_case` names that say what's inside.
- **Hot paths never scan the ledger.** Anything a hook or a status command reads goes through
  `ledgerindex` (index first, `history.ReadTail` window as fallback), never `history.ReadLast(path, 0)`.
  Records are written with `ledgerindex.Append`, not `history.Append`. The 20 ms budget is tested in
  `internal/ledgerindex` (a 100,000-record ledger; skipped under `-short` and `-race`).
- **Test with real things.** Spawn real processes, take real locks, push to real (temporary) git repos.
  Fake `/proc` only where a real process can't produce the case (see `procinfo_test.go`).
- **Tests never depend on the load of the machine they run on.** heavyq stamps the machine reading (load,
  free RAM) on every ledger record and classes a failure that started over a limit as `under_load`, so a
  busy CI box or laptop would turn a test's plain failure into one. The e2e rig's `baseConfig` is therefore
  calm (`job_mb = 1`, `light_mb = 1`, `max_load_ratio = 64`: a value of 0 means "the default", so use 1);
  a test that asserts a failure class, a red gate, a loop or a proof starts from it and never drops those
  keys. To assert `under_load` on purpose, either use `loadedConfig` (a RAM budget no machine has) or pin
  the reading with the test-only override: `HEAVYQ_TEST=1 HEAVYQ_TEST_FAKE_METRICS="load1=9,free_mb=99999,cores=8"`
  (rig helper `h.fakeMetrics`, in `internal/metrics/test_override.go`). It is honored only when
  `HEAVYQ_TEST` is exactly `1` and every key parses (`load1`, `free_mb`, `total_mb`, `cores`); anything else
  is ignored and the real machine is read. Never set it outside tests. To check a change, run the affected
  tests in a loop while the machine is busy (`stress-ng --cpu 0`, or a few `while :; do :; done` shells).
- **Agent output is a budget.** Anything a run prints in agent mode costs an agent context: keep the digest
  under 4 KB, `why` under 600 bytes, the `help agents` card under 900. The tests in `docs/agent-mode.md` enforce it.
  The e2e rig strips `CLAUDECODE` and friends from the child environment; a test of agent mode adds one back.
- **`go test -race` must stay clean.** Use the `lockedBuf` / `syncBuffer` helpers when a test reads
  output that a process is still writing.
- **Fail closed.** If a value is unsafe or unknown (a config value, a remote name, a path), refuse it and
  say why. Never guess and run.
- **Exit codes are API.** Don't change the meaning of 2, 75, 76, 77, 78 or 255.
- **The hook fails open.** `heavyq hook claude-pretooluse` must never block an agent: on any doubt it
  leaves the command unchanged (exit 0, no output) and logs. New command shapes need table tests in
  `internal/hook/claude_pretooluse_test.go`; output changes need `go test ./internal/hook -update`.

## Fuzzing

The parsers that read input heavyq does not control have Go fuzz tests (`Fuzz*` in `*_fuzz_test.go`): the
hook's shell-command parser and rewrite, `internal/redact`, `internal/anchor`, `heavyq.toml`, the user config, and the
ledger reader. CI fuzzes each for 20 s on pull requests that touch its package and for 2 minutes every night.

```bash
go test ./internal/anchor -run '^$' -fuzz '^FuzzSetChecked$' -fuzztime 30s   # one target at a time
go clean -fuzzcache                                                          # fuzzing fills the cache
```

A crasher lands in `testdata/fuzz/<FuzzName>/`. Fix the parser, then commit that file with the fix: it runs as
a regression test on every plain `go test`. A new parser that reads untrusted input gets a fuzz target and an
entry in the `fuzz` job of `.github/workflows/ci.yml`.

## Good first issues

Look for the [`good first issue`](https://github.com/LysCongThanh/heavyq/labels/good%20first%20issue)
label. Each one names the files to touch and how to test it. Comment on the issue before you start, so two
people don't do the same work.

## Pull requests

- One topic per PR, with a test that fails without the change.
- The PR title is a conventional commit (`feat:`, `fix:`, `docs:`, `test:`, `refactor:`, `ci:`, `build:`);
  a check enforces it, and it becomes the squash commit.
- A new command is two new files and no edit to an existing one: `<name>_command.go` (the handler) and
  `<name>_command_spec.go` (an `init()` calling `registerCommand` with its table entry, an `Order` that places it
  in the usage, and a `Run` adapter; copy a small one such as `trust_command_spec.go`). There is no usage text,
  dispatch switch or command list to edit: they are generated from the table. A new subcommand or flag is an edit
  to its command's spec file; `go test ./internal/cli` says what is missing. A change to a config key, a
  validation rule or the status snapshot needs `cd tools/schemagen && go run .` and the updated `schema/`
  committed (CI checks).
- If you change behaviour, update the docs (README, docs/queue.md, docs/configuration.md, docs/exit-codes.md) in
  the same PR, and add a **changelog fragment**: `changelog.d/<pr-or-slug>.<kind>.md` with `kind` one of `added`,
  `changed`, `fixed`, `security` and the release-notes entry as its text (see `changelog.d/README.md`). Do not
  edit `CHANGELOG.md`: that file is what every parallel PR would conflict on. A PR that touches `internal/` or
  `cmd/` code without a fragment fails the `changelog` check unless it carries the label `no-changelog`.
  At release time the maintainer runs `go run ./tools/changelog release <version>`, which folds the fragments
  into `CHANGELOG.md` and deletes them; the section becomes the release notes.

## CI tiers

Every pull request runs the **fast tier** (under 5 minutes, `.github/workflows/ci.yml`): `test` (the required
check: gofmt, vet, build, the ledger latency gate and every unit and end-to-end test through the gate `fast` in
`heavyq.toml`, without `-race`), `lint`, `schemas`, `gitleaks`, a 20-second fuzz of the packages the PR touched
(`.github/fuzz-targets.txt`), and the `changelog` and PR-title checks. A PR that changes only documentation skips
the Go jobs, and `test` still reports. The **full tier** (`ci-full.yml`: the whole suite under `-race`, macOS,
the release snapshot, `install.sh` on both platforms, govulncheck, the 2-minute fuzz of every target, doc links)
runs on every push to `main`, nightly, and on a PR that carries the label `ci:full`. Label a PR `ci:full` when it
touches locks, process handling, the queue or anything macOS-specific, and before a release. Pushes to a PR
cancel its running jobs.

The end-to-end tests drive a heavyq binary that `TestMain` builds with `-race`; that is what makes them take about
9 minutes. `HEAVYQ_TEST_NORACE=1` builds it without (about 85 seconds), which is what the fast tier sets. Locally,
leave it unset when you touch concurrency.

## Reporting security issues

See [SECURITY.md](SECURITY.md). Never in a public issue.

## Code of conduct

This project follows the [Contributor Covenant](.github/CODE_OF_CONDUCT.md).

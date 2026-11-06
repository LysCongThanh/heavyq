# Gates: `heavyq.toml`

A repository describes what "proven" means for it in `heavyq.toml` at its repository root (the work tree
top level; in a linked worktree, that worktree's own copy). A **gate** is a named list of shell commands that
must all pass on the current code.

`heavyq gate status` tells you whether the code checked out now is proven, and `heavyq gate run`
proves it. `gate status` reads the run ledger, answers in about 20 ms and runs nothing. Without a
`heavyq.toml`, a default gate is [detected](#no-heavyqtoml-the-detected-gate).

## No `heavyq.toml`: the detected gate

A repository without `heavyq.toml` still has a gate. heavyq makes up one called `ready` from the files at the
git root, in this order, and every ecosystem found adds its commands to it:

| File at the git root | Commands added |
|---|---|
| `go.mod` | `go vet ./...`, `go test ./...` |
| `package.json` | `<pm> run <script>` for each of `typecheck`, `lint`, `test` that it defines, in that order |
| `pytest.ini`, `pyproject.toml`, or `setup.cfg` containing a `[tool:pytest]` section | `pytest` |
| `Cargo.toml` | `cargo test` |

`<pm>` comes from the lockfile next to `package.json`: `pnpm-lock.yaml` is `pnpm`, `yarn.lock` is `yarn`,
`bun.lockb` or `bun.lock` is `bun` (checked in that order), anything else is `npm` (`npm run test`). A script
counts only if its value is a non-blank string, and the `test` script that `npm init` writes
(`echo "Error: no test specified" && exit 1`) does not count, because it can never pass.

Detection never runs anything. It reads at most a few files, each bounded to 1 MiB (a bigger one is ignored),
parses `package.json` as JSON data, and uses only the *names* of the scripts: no text from a file reaches a
command line, so a repository cannot choose what the detected gate runs beyond "its own `lint` script".
A file that is missing, malformed or not a regular file adds nothing. The detected gate has the default `prio`
(3), is not `light` and runs `local`.

It is used by `gate status`, `gate run` and the `why` sentence, which call it `ready (detected)`, and
`gate status --json` adds `"detected": true` (the key is absent for a gate from a file). A `heavyq.toml`
always wins: if the file exists, even empty or invalid, nothing is detected. When there is no file and nothing
to detect, the commands exit 2 and say so. The detected gate is run only by an explicit `gate run`, like any
other.

```
$ heavyq gate status
gate ready (detected): NOT PROVEN on tree 30275da9949f
  STATE    WHEN                 LANE              RUN  COMMAND
  missing  -                    -                   -  go vet ./...
  missing  -                    -                   -  go test ./...
  0 failed, 2 missing. Prove it with: heavyq gate run ready
```

## `heavyq init [--agents-md] [--force] [--print]`

Writes the detection down as `heavyq.toml` at the git root (from any subdirectory), so it can be reviewed,
edited and committed. The file is commented: what it is, the trust model below, and how to add a gate. With
nothing to detect, it carries a commented-out example instead of a gate, and `init` says so.

- It never overwrites: if `heavyq.toml` exists (a symlink counts), `init` leaves it alone and exits **1**.
  `--force` replaces it (a symlink is replaced, never written through). `--print` writes the file to stdout
  and nothing to disk, and does not care whether one exists.
- `--agents-md` also puts a short section into `AGENTS.md` (created if missing) and `CLAUDE.md` (only if it
  already exists), between `<!-- heavyq:begin -->` and `<!-- heavyq:end -->`: before saying a task is done,
  run `heavyq gate run ready`; exit 1 means fix and re-run; exit 75 means the queue is busy, retry later and
  never bypass heavyq. Text outside the markers is never touched. A file without markers gets the section
  appended; one with a well-formed pair gets it replaced in place, so running `init --agents-md` again changes
  nothing. Unpaired, repeated or reversed markers, a file over 1 MiB, or a symlink that points outside the
  repository are refused (exit 1). It is applied even when `heavyq.toml` already exists, so a hand-written
  `heavyq.toml` can get the section without `--force`.
- `--print` cannot be combined with `--agents-md`. Outside a git work tree, or with unknown flags or
  arguments, `init` exits 2.

`init` is safe for an agent to run: it reads files to detect, writes only `heavyq.toml`, `AGENTS.md` and
`CLAUDE.md` at the git root, and runs nothing. The section names the gate `ready`, the name the detected gate
and the generated file use.

## Trust model

`heavyq.toml` is trusted like a **Makefile**: it holds commands, and whoever can edit it decides what they
are. Because of that:

- Gates run only on an explicit `heavyq gate run`. Hooks never execute gates.
- The file is parsed strictly. Unknown keys, wrong types and out-of-range values are errors, never
  ignored. A typo must not silently weaken a gate.
- Commands are run through `sh -c`, so quoting, pipes and `&&` work as in a shell. Where `bash` exists the
  command runs under `bash -o pipefail`, so `false | tail` fails the command (without pipefail the exit status
  would be `tail`'s, and a missing tool in the first stage would pass). Without bash the plain `sh` runs it.
  A gate run is also strict about a missing command: exit 0 with a shell "command not found" line in the
  output fails with 127 and proves nothing (see [exit-codes.md](exit-codes.md)).

## Format

```toml
[[gate]]
name   = "ready"                       # required
run    = ["gofmt -l . | (! grep .)",   # required, non-empty, run in order
          "go vet ./...",
          "go test -race ./..."]
prio   = 3                             # optional, 1-9, default 3
light  = false                         # optional, may use the light slots
remote = "local"                       # optional: local (default) | auto | <remote name>
needs  = ["pg", "port:2"]              # optional: pool values each command holds while it runs (see pools.md)
with   = ["shared-schema@v2.1.0"]      # optional: sibling repos next to this one, pinned (see remote.md)
need_gh = false                        # optional: the commands use this machine's GitHub CLI login: run here only

[proof]                                # optional: where proofs run (gates without a remote key, item done --run)
remote = "auto"                        # local (default) | auto | <remote name>

[anchor]                               # reserved: accepted, not used yet
file  = ".heavyq/anchor.md"
extra = ["plans/*/phase-*.md"]

[items]                                # optional: limits of the claim size gate (docs/items.md)
iterations      = 3                    # proof runs an item is expected to need, 1-20
max_proof_share = 1.0                  # share of the 30 min lease the expected proof time may use, (0, 1]
```

| Key | Type | Default | Rules |
|---|---|---|---|
| `gate.name` | string | required | `[a-z0-9][a-z0-9_-]{0,39}`, unique in the file |
| `gate.run` | list of strings | required | non-empty list; each string non-blank, at most 2000 bytes, no NUL |
| `gate.prio` | integer | `3` | 1-9; 1 runs first |
| `gate.light` | bool | `false` | |
| `gate.remote` | string | `"local"` | `local`, `auto`, a remote name (`[A-Za-z0-9][A-Za-z0-9._-]{0,63}`), or an ordered list `"a,b,c"` of `local` and remote names (the first with room, see [remote](remote.md#an-ordered-list-of-machines)). Whether a remote exists is checked against your own config when the gate runs. |
| `gate.needs` | list of strings | none | `pool` or `pool:N` entries, each pool once; every command of the gate holds them while it runs, see [pools](pools.md). With `needs`, `remote` must be `local`, `auto` or a list that names `local` (the run stays local). |
| `gate.with` | list of strings | none | sibling repositories each command needs next to this one: `"name"` or `"name@ref"` (a directory next to the repository, or a path containing `/`). Passed to every command as `--with`: pinned at the start of the run, shipped with remote runs, and part of the proof, so a sibling at another commit is another run, and a sibling that cannot be resolved proves nothing. See [remote](remote.md#sibling-repositories---with). |
| `gate.need_gh` | bool | `false` | the commands call the GitHub CLI with this machine's login, so the gate runs here only (passes `--need-gh`). Not with a named `remote`; it overrides a `[proof] remote` default, and `--remote <name>` on the command line is exit 2. |
| `pool` | tables | none | `[[pool]]` tables, as in the user config; a pool here replaces the user's pool of the same name. See [pools](pools.md). |
| `proof.remote` | string | unset | `[proof]`: where `item done --run` and `gate run` run proofs for gates without their own `remote` key; `local`, `auto`, a remote name or an ordered list `"a,b,c"`. Overrides `[proof]` of the user config. See [items](items.md#where-the-proof-runs). |
| `anchor.file` | string | none | relative path inside the repository |
| `anchor.extra` | list of strings | none | relative globs inside the repository |
| `items.iterations` | integer | `3` | 1-20; proof runs an item is expected to need; `item claim` refuses an item whose proof p50 times this is over the lease budget |
| `items.max_proof_share` | number | `1` | above 0, at most 1; the share of the 30 minute lease that budget is |

The file may be empty (no gates). It is at most 1 MiB and must be a regular file (a symlink to one is fine).

## Errors

Every error names the file. Unknown keys also name the line when it can be found:

```
/work/app/heavyq.toml:8: unknown key "gate.light_job"
/work/app/heavyq.toml: [[gate]] #2 ("go"): prio 10 must be between 1 and 9
/work/app/heavyq.toml: [[gate]] #3: duplicate gate name "ready"
```

A directory that is not in a git work tree, or a repository without `heavyq.toml` and with nothing to detect,
simply has no gates. That is not an error for `heavyq why`; `gate status` and `gate run` exit 2 and say so.

## What "proven" means

Proof is derived from the run ledger (`heavyq history`); there is no separate store. A gate command is
**proven** on the code checked out now when the runs of it on this tree agree that it passes. A ledger record
is a run of the command when all of these hold:

- same repository (all work trees of a clone share one id), and the same **tree id**: `HEAD`'s tree when the
  work tree is clean, `<HEAD tree>+<digest of the edits>` when it is dirty, both without heavyq's own
  bookkeeping files (see **What the tree id ignores**). Any other edit changes the id and undoing it restores
  the old one, so a revert is proven again at once;
- it is a run of the same command in the same environment, see **Run identity** below: the exact
  `sh -c '<command>'` (under pipefail, see above) from the repository root, with the same values of the `cache.env` variables and the
  same base ref;
- it ran to its end: runs that gave up, were cancelled or were interrupted are inconclusive and neither prove
  nor fail anything. Exit 0 passes; any other exit code is a failure. A `--cache` hit or a run that reused an
  identical in-flight run counts, because the ledger only gives those the tree they proved.

### What the tree id ignores

heavyq writes to the work tree itself: `heavyq item done` ticks a box in the anchor file. If that changed the
tree id, the proof that justified `done` would read as stale the moment it was recorded. So the tree id leaves
out a fixed set of bookkeeping paths, in the committed tree and in the edits alike:

- the anchor file (`[anchor] file` in `heavyq.toml`, default `.heavyq/anchor.md`);
- `.heavyq/decisions/` and `.heavyq/stances/`.

Ticking an item, adding a decision or committing either one does not change the id, and an uncommitted change
to these paths alone does not make the tree dirty (the result cache can still answer). Everything else counts,
`heavyq.toml` included: changing a gate must void its proof. Other files under `.heavyq/` count too.

How the committed part is computed: a repository without those paths keeps `HEAD`'s tree id as is. Otherwise
heavyq reads only the tree levels on the way to them (the root and `.heavyq/`, a handful of entries) and
hashes the listing without the excluded entries; unchanged subtrees keep their own ids, so the cost does not
grow with the number of files. The result is the id git would give that tree, so a commit that differs from
another only in bookkeeping files has the same id as it, but it is a heavyq identity, not a promise that the
object exists: nothing is written to the repository. The edits part leaves the same paths out of the diff and
of the untracked listing.

**Other checkouts inside the work tree.** The untracked listing also leaves out every directory that holds
its own `.git` (a linked work tree of this repository, a clone, any nested repository) and everything under
`.claude/worktrees/`, where Claude Code puts the work trees of its agents. Their files are not files of this
checkout, so an agent editing there never makes the main checkout dirty or unprovable. A submodule recorded in
the index is not one of these: changes inside it still make the tree unknown. `git status` still lists such
directories; `heavyq doctor` and `heavyq init` print the line that hides them from it:
`echo '.claude/worktrees/' >> "$(git rev-parse --git-path info/exclude)"`.

### Run identity

Every ledger record carries a `run_id`, the single definition of "the same run" that the result cache, proof,
flaky detection and the Tower's loop detection share (`internal/runident`). It is the sha256 of:

| Component | Notes |
|---|---|
| tree id | `HEAD`'s tree, or `<tree>+<digest of the edits>` when dirty, without bookkeeping files (above) |
| exact argv | NUL-joined, before redaction and truncation (this is `cmd_hash`) |
| subdirectory | relative to the repository root; gate commands run from the root |
| `cache.env` | for each listed variable its value, or "unset" (set-but-empty differs from unset) |
| base ref | the commit of `--base-ref` / `queue.base_ref`; a ref that does not resolve counts as none |
| `--with` siblings | name and full pinned commit of each, in any order |

Where the run executed (local or a remote) is not part of it, nor is the lane, the priority or the heavyq
version (the cache key adds the version on top).

Matching rules for a gate command, in order:

1. The record has a `run_id` and so does the gate command: they match if and only if the ids are equal.
   `gate status` computes the gate's id from the tree it identified, the repository root, the `cache.env`
   allowlist read from **its own environment**, and `queue.base_ref`: the same inputs `gate run` gives the
   run. So `FEATURE_X=1 heavyq gate run` proves the gate only for a shell where `FEATURE_X=1`, and
   `heavyq gate status` without it says `missing` (`ran on this tree with a different environment
   (cache.env) or base ref: run it again`).
2. Otherwise (a record from an older heavyq without `run_id`): it matches by `cmd_hash`, or on the oldest
   lines by the redacted command string, **only when `cache.env` is empty**, because then no variable can make
   two runs of one argv differ. With a non-empty allowlist such a record never matches (fail closed): it
   cannot say which values it ran with. `gate status` reports `missing` with the reason, and one `gate run`
   records a `run_id`.

A failure under another environment is not a failure of this one: it does not fail the command and does not
make it flaky. A record whose tree is unknown has no `run_id` and never proves anything.

Each command is classified by those runs. The first row that applies wins:

| Class | When | State |
|---|---|---|
| `under_load` | the latest run failed, and the machine was over its limits when that run was admitted | `failed` |
| `flaky` | the command both **failed** (not explained by load, below) and **passed** on this tree, and the latest run failed, or only one pass follows the failure | `failed` |
| `flaky` | same, and the last two runs on this tree passed | `proven` |
| `failed` | the latest run failed, and it never passed on this tree | `failed` |
| (none) | the latest run passed, and no failure on this tree is unexplained | `proven` |

- **Flaky is a label, and proof recovers by repetition.** A command that failed and passed on the same tree
  is proven once its last two runs passed, and the label stays (`proven (flaky: 1 fail / 2 pass on this
  tree)`). Until then it is not proven, and `gate status` says `flaky — re-run once to confirm:
  heavyq gate run <name>`. Changing the code is never needed to get there. `gate run` re-runs a flaky command
  for real (without `--cache`), because a replayed pass is the same run again. The counts (`passes`, `fails`)
  stay visible when it is proven. They count runs: a cache hit or coalesced result repeats a run that already
  has its own line, so it is neither counted nor taken as a repetition.
- **Under load.** A failure is "under load" when its ledger record says the run was admitted past the resource
  gate by the anti-starvation bypass (`forced`), or that at admission the 1-minute load was at or above the
  machine's limit (`max_load_ratio` x cores), or less RAM was free than the job's budget (`job_mb`, or
  `light_mb` for `--light`). The reading is the one the scheduler judged: the local machine, or for a remote
  the probe placement used. A failure under load is still a failure, so the command is not proven while it is
  the latest run, but it is not evidence of flakiness and it never breaks a repetition: if the rerun passes,
  the command is proven. Rerun once before you debug.
  A record without a machine reading (an older line, a result replayed from the cache, a platform that cannot
  be measured) is never called under load.

A gate is proven when every command is. Everything else fails closed:

- A command with no matching record is `missing`.
- If the tree cannot be identified (not a repository, no commit yet, a submodule with
  changes of its own, more untracked content than heavyq can digest) nothing is provable. `gate status` says
  why and exits 1, and `gate run` refuses to start.
- An older record without `run_id` does not prove a command when `cache.env` is set (see Run identity).
- A command longer than 200 bytes whose only runs are older records (no `cmd_hash`) is `missing`: the ledger
  only kept a cut-off form of it, and two different long commands could look alike. Run it again (`gate run`)
  and its hash is recorded.
- Only the last 20,000 ledger records are read. An older proof counts as missing, and `gate run` then
  re-runs it, which the result cache usually answers at once.
- An unreadable ledger proves nothing.

## `heavyq gate status [name] [--json]`

`name` defaults to `ready`, else to the only gate in the file. Exit code: **0** every command proven,
**1** not proven, **2** usage error (neither `heavyq.toml` nor a detectable gate, no such gate, an invalid
file, not in a git work tree).

```
gate ready: NOT PROVEN on tree 30275da9949f+1c9be0e2 (uncommitted edits)
  STATE    WHEN                 LANE              RUN  COMMAND
  proven   2026-11-02 00:39:05  agent-3          0.0s  gofmt -l . | (! grep .)
  failed   2026-11-02 00:42:24  agent-3          9.4s  go vet ./...  (exit 1)
  missing  -                    -                   -  go test -race -count=1 ./...
  1 failed, 1 missing. Prove it with: heavyq gate run ready
```

A failure that is flaky or happened under load says so on its line, and the summary counts them:

```
  failed   2026-11-02 00:44:10  agent-3         31.2s  go test -race -count=1 ./...  (exit 1; flaky: 1 fail / 1 pass on this tree)
  failed   2026-11-02 00:44:52  agent-3          9.1s  go vet ./...  (exit 1; under load: load 14.20 >= 12.00 at start)
  2 failed (1 flaky, 1 under load), 0 missing. Prove it with: heavyq gate run ready
  under load: the machine was over its limits when the run started; rerun once before debugging: heavyq gate run ready
  flaky — re-run once to confirm: heavyq gate run ready
```

`heavyq why` closes with the same counts: `The ready gate is not proven on this tree (tree 30275da9949f):
2 failed (1 flaky, 1 under load), 0 missing. Prove it with: heavyq gate run ready`.

`RUN` shows `hit` or `coalesced` when the deciding run was answered from the cache. `--json` prints one
object (additive changes only):

```json
{
  "gate": "ready",
  "proven": false,
  "tree": "30275da9949f...+1c9be0e2...",
  "commit": "e374faa...",
  "dirty": true,
  "commands": [
    {"cmd": "gofmt -l . | (! grep .)", "state": "proven", "at": "2026-11-02T00:39:05Z",
     "lane": "agent-3", "run_secs": 0.04, "cache": "miss", "exit": 0, "machine": "local"},
    {"cmd": "go vet ./...", "state": "failed", "class": "failed", "at": "2026-11-02T00:42:24Z", "lane": "agent-3",
     "run_secs": 9.4, "exit": 1, "machine": "local", "fails": 1},
    {"cmd": "go test -race -count=1 ./...", "state": "missing", "run_secs": 0}
  ]
}
```

| Field | Meaning |
|---|---|
| `gate`, `proven` | the gate and whether every command is proven |
| `detected` | `true` only when the gate is the default heavyq detected because there is no `heavyq.toml`; absent otherwise |
| `tree`, `commit`, `dirty` | the code asked about; `tree` is `""` when it cannot be identified (`commit` is omitted before the first commit) |
| `reason` | set when nothing can be proven at all (unknown tree); omitted otherwise |
| `commands[].cmd` | the command string from `heavyq.toml` (secrets redacted) |
| `commands[].state` | `proven`, `failed` or `missing`. A flaky command is `proven` once its last two runs passed, else `failed` |
| `commands[].class` | `flaky` (with either state), or, when `state` is `failed`, `under_load` or `failed` (see above); omitted otherwise |
| `commands[].at`, `lane`, `run_secs`, `cache`, `exit`, `machine` | the deciding run; all but `run_secs` are omitted for `missing`. For a `flaky` command that is not proven yet after a pass, it is the latest failure that load does not explain. `cache` is `hit`, `coalesced`, `miss` or `skip` and is omitted without `--cache` |
| `commands[].passes`, `fails` | runs on this tree that exited 0 / did not (cache replays are not counted again); each omitted when 0 |
| `commands[].reason` | why a command is `missing` (`tree unknown`, too long to match), or the evidence for its class: `flaky: 1 fail / 2 pass on this tree`, `under load: load 14.20 >= 12.00 at start`, `under load: 800 MB free < 3072 MB job budget at start`, `under load: admitted past a resource limit by the anti-starvation bypass` |

## `heavyq gate run [name] [--parallel] [--keep-going] [--lane NAME] [--remote WHERE]`

For each command that `gate status` would not call proven, in file order, heavyq runs the equivalent of

```
heavyq <prio> --cache --gate <name> --remote <remote> [--light] [--lane NAME] --strict -- sh -c '<command>'   # no --cache for a flaky command
```

from the repository root, with the gate's `prio`, `light` and `remote`. The machine is, first set wins:
`--remote`, the gate's `remote` key, `[proof] remote` in `heavyq.toml`, `[proof] remote` in the user config,
`local` (see [where the proof runs](items.md#where-the-proof-runs)). It goes through the same queue,
resource gate, result cache and in-flight coalescing as any other run, and every command is recorded in the
ledger with `gate = <name>`. The lane is `--lane`, else `$HEAVYQ_LANE`, else the repository directory name;
`$HEAVYQ_SESSION` and `$HEAVYQ_ITEM` tag the runs as usual.

- Commands already proven on this tree are not run: heavyq prints `proven: <command>` and moves on.
- It stops at the first failing command (the rest are listed as `not run`), unless `--keep-going`.
  `--parallel` starts all unproven commands at once (the queue's slots still limit how many run) and
  implies `--keep-going`. Each command runs as its own child `heavyq` process (the same invocation as above,
  with the same `--config`), so every ticket has its own pid: `heavyq cancel <pid>` stops exactly one command.
  Their output is streamed line by line with a `[cmd N]` prefix (N is the command's number in the gate, as in
  `[N/total]`). The children get no stdin; a SIGINT, SIGTERM or SIGHUP sent to `gate run` is relayed to all of
  them. Without `--parallel` the commands run one at a time inside the `gate run` process.
- Dirty trees are never cached, so on uncommitted edits every unproven command really runs. The dirty tree is
  proven all the same, and stays proven until the next edit.
- It ends by printing the status table. **Exit 0** only if the ledger now proves the whole gate on the code
  that is checked out at that moment. **1** otherwise: a command failed, the work tree changed while the
  gate ran (the results belong to the earlier code; heavyq says so), or the tree cannot be identified. A stop
  by a signal (128+n) or an operator cancel (78) is passed through. Usage errors are 2.
- A `remote` that heavyq's own config does not define fails the command when it runs, not when the file is
  read.
- Commands that write into the work tree (a build output that is not git-ignored) change the tree under
  their own feet, so their results can never be proven. Ignore such outputs, or build outside the tree.

## Ledger fields behind it

`heavyq history` lines gain these fields (additive; older lines load as before and simply lack them):

| Field | Meaning |
|---|---|
| `run_id` | sha256 (hex) of the run identity: tree id, `cmd_hash`, subdir, `cache.env` values, base ref commit (see Run identity). Absent on older lines and when the tree is unknown |
| `cmd_hash` | sha256 (hex) of the exact argv the run executed, joined with NUL, computed before redaction and truncation. `cmd` stays the redacted, 200-byte display form. The hash is of the unredacted command, so it could confirm a guess at a secret inside one: the ledger file is 0600, keep it private |
| `load1`, `free_mb`, `cores` | the machine when the run was admitted: 1-minute load, available RAM, cores. For a local run it is the sample the resource gate judged, for a remote run the probe placement used. Absent when the machine cannot be measured, so `cores` > 0 marks a reading (a missing `load1` or `free_mb` beside it is a real 0) |
| `max_load`, `job_mb` | the limits applied to that reading: load at or above `max_load` (`max_load_ratio` x cores), or free RAM below `job_mb` (`light_mb` for `--light`), is "over its limits" |
| `forced` | admitted past the resource gate by the anti-starvation bypass (existing field) |

Runs replayed from the cache, and tickets that never ran, carry no machine reading.

## Gates as the proof of an item

`proof: gate:<name>` in the anchor file is the form to prefer over `proof: <shell command>`. The gate's
commands sit in `heavyq.toml`, where a person or an agent harness can read them before running anything, and
`heavyq gate run <name> --remote linuxi` (or `heavyq item done <id> --run --remote linuxi`, or `[proof] remote`)
needs no `sh -c '...'` typed by hand, which a harness that vets commands refuses. "The test must exist and
pass" is one command: `go test -run` that matches no test exits 0, so pipe it to a check for the test's own
line:

```toml
[[gate]]
name = "size-gate"      # the item's id
run  = ["go test -count=1 -run '^TestE2EClaimRefusesOversizedItem$' -v ./internal/cli | grep -q -- '--- PASS: TestE2EClaimRefusesOversizedItem'"]
```

`heavyq preflight` warns about an item whose proof needs a shell and names this form.

## heavyq proves itself

This repository's [`heavyq.toml`](../heavyq.toml) defines `ready` as `gofmt -l . | (! grep .)`,
`go vet ./...` and `go test -race -count=1 ./...`. CI builds heavyq and runs `./heavyq gate run ready`, so
a local proof and the CI proof are the same thing. Locally, `heavyq gate status` after your last edit tells
you whether the gate has already passed on exactly this code. The anchor items of the repository
([`.heavyq/anchor.md`](../.heavyq/anchor.md)) are proven by gates of the same file, one per item with the
item's name.

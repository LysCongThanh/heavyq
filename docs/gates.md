# Gates: `heavyq.toml`

A repository describes what "proven" means for it in `heavyq.toml` at its repository root (the work tree
top level; in a linked worktree, that worktree's own copy). A **gate** is a named list of shell commands that
must all pass on the current code.

`heavyq gate status` tells you whether the code checked out now is proven, and `heavyq gate run`
proves it. Without a `heavyq.toml`, a default gate is [detected](#no-heavyqtoml-the-detected-gate).

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
- Commands are run through `sh -c`, so quoting, pipes and `&&` work as in a shell.

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

[anchor]                               # reserved: accepted, not used yet
file  = ".heavyq/anchor.md"
extra = ["plans/*/phase-*.md"]
```

| Key | Type | Default | Rules |
|---|---|---|---|
| `gate.name` | string | required | `[a-z0-9][a-z0-9_-]{0,39}`, unique in the file |
| `gate.run` | list of strings | required | non-empty list; each string non-blank, at most 2000 bytes, no NUL |
| `gate.prio` | integer | `3` | 1-9; 1 runs first |
| `gate.light` | bool | `false` | |
| `gate.remote` | string | `"local"` | `local`, `auto`, or a remote name (`[A-Za-z0-9][A-Za-z0-9._-]{0,63}`). Whether the remote exists is checked against your own config when the gate runs. |
| `anchor.file` | string | none | relative path inside the repository |
| `anchor.extra` | list of strings | none | relative globs inside the repository |

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
**proven** on the code checked out now when the latest *conclusive* ledger record that matches it passed.
A record matches when all of these hold:

- same repository (all work trees of a clone share one id), and the same **tree id**: `HEAD`'s tree when the
  work tree is clean, `<HEAD tree>+<digest of the edits>` when it is dirty. Any edit changes the id and
  undoing it restores the old one, so a revert is proven again at once;
- it ran the exact command heavyq runs for the gate, `sh -c '<command>'`, from the repository root (recorded
  with the usual secret redaction);
- it ran to its end: runs that gave up, were cancelled or were interrupted are inconclusive and neither prove
  nor fail anything. Exit 0 proves; any other exit code is a failure. A `--cache` hit or a run that reused an
  identical in-flight run counts, because the ledger only gives those the tree they proved.

Of the matching conclusive records the **latest** decides, so a command that passed and then failed on the
same tree is `failed`. A gate is proven when every command is. Everything else fails closed:

- A command with no matching record is `missing`.
- If the tree cannot be identified (not a repository, no commit yet, a submodule or nested repository with
  changes of its own, more untracked content than heavyq can digest) nothing is provable. `gate status` says
  why and exits 1, and `gate run` refuses to start.
- A command whose recorded form heavyq would have shortened (more than 200 bytes) cannot be matched exactly
  and stays `missing`, with that reason. Keep gate commands short, or put the long part in a script.
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
    {"cmd": "go vet ./...", "state": "failed", "at": "2026-11-02T00:42:24Z", "lane": "agent-3",
     "run_secs": 9.4, "exit": 1, "machine": "local"},
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
| `commands[].state` | `proven`, `failed` or `missing` |
| `commands[].at`, `lane`, `run_secs`, `cache`, `exit`, `machine` | the deciding run; all but `run_secs` are omitted for `missing`. `cache` is `hit`, `coalesced`, `miss` or `skip` and is omitted without `--cache` |
| `commands[].reason` | why a command is `missing`, when heavyq knows (`tree unknown`, too long to match) |

## `heavyq gate run [name] [--parallel] [--keep-going] [--lane NAME]`

For each command that `gate status` would not call proven, in file order, heavyq runs the equivalent of

```
heavyq <prio> --cache --gate <name> --remote <remote> [--light] [--lane NAME] -- sh -c '<command>'
```

from the repository root, with the gate's `prio`, `light` and `remote`. It goes through the same queue,
resource gate, result cache and in-flight coalescing as any other run, and every command is recorded in the
ledger with `gate = <name>`. The lane is `--lane`, else `$HEAVYQ_LANE`, else the repository directory name;
`$HEAVYQ_SESSION` and `$HEAVYQ_ITEM` tag the runs as usual.

- Commands already proven on this tree are not run: heavyq prints `proven: <command>` and moves on.
- It stops at the first failing command (the rest are listed as `not run`), unless `--keep-going`.
  `--parallel` starts all unproven commands at once (the queue's slots still limit how many run) and
  implies `--keep-going`; their output interleaves.
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

## heavyq proves itself

This repository's [`heavyq.toml`](../heavyq.toml) defines `ready` as `gofmt -l . | (! grep .)`,
`go vet ./...` and `go test -race -count=1 ./...`. CI builds heavyq and runs `./heavyq gate run ready`, so
a local proof and the CI proof are the same thing. Locally, `heavyq gate status` after your last edit tells
you whether the gate has already passed on exactly this code.

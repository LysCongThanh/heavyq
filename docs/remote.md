# Spare machines

Add a remote in `~/.config/heavyq/config.toml`:

```toml
[[remote]]
name = "buildbox"     # used in --remote buildbox and in status
ssh = "buildbox"      # an alias from ~/.ssh/config (key auth; ControlMaster recommended)
heavy_slots = 2
light_slots = 2
cache_hook = '''
# optional: restore node_modules from a cache keyed by the lockfile
[ -f package-lock.json ] || exit 0
key=$(sha256sum package-lock.json | cut -c1-16); c="$HEAVYQ_CACHE_DIR/nm-$key"
if [ -d "$c" ]; then cp -al "$c" node_modules; exit; fi
npm ci --no-audit --no-fund && { cp -al node_modules "$c.$$" && mv -T "$c.$$" "$c" || rm -rf "$c.$$"; } 2>/dev/null; true
'''
```

Then:

```bash
heavyq 3 --remote buildbox -- npm test        # always there
heavyq 3 --remote auto -- npm test            # best score; you vouch the command is self-contained
heavyq 3 -- npm test                          # best score only if the repo is clean and paths stay inside it
heavyq 3 --remote local -- npm test           # never leave this machine
heavyq 3 --remote fast,local,buildbox -- npm test   # the first of these with room, in this order
heavyq item done login-api --run --remote auto   # a proof placed like any other run; or set [proof] remote once
heavyq 3 --needs-ram -- npm run e2e           # place by free RAM only
heavyq 3 --remote buildbox -- npm run lint:changed                          # origin's default branch (origin/main) is there too
heavyq 3 --remote buildbox --base-ref origin/develop -- npm run lint:changed   # another base ref instead
heavyq 3 --remote buildbox --base-ref none -- npm test                      # no base ref at all
heavyq 3 --remote buildbox --with ../shared-lib@v2.1.0 -- npm test          # sibling repo pinned to a commit
heavyq 3 --tools envsubst,psql -- bash scripts/e2e.sh   # only on a machine where both resolve (see below)
```

What a remote run sees, and what it doesn't:

- **Only committed code.** The exact SHA of `--ref` is pushed with `git push` (it doesn't need to be on
  GitHub). Uncommitted edits and ignored files (`.env.test`, `dist/`) stay behind.
- **Edits left behind are named.** A run admitted to a remote while the work tree holds uncommitted changes to
  tracked files, or untracked files that git does not ignore, says how many files it leaves behind, once. A run
  that passes on the shipped files says nothing about a test file that exists only as an edit, so the line is the
  reminder. A person gets a line of its own
  (`heavyq: warning: 3 uncommitted files are not shipped: buildbox runs the committed tree only (commit them, or use --remote local)`);
  in [agent mode](agent-mode.md#what-a-run-prints) it is the end of the running line
  (`..., tree 3f2a9c1d7b0e, 3 uncommitted files not shipped)`), so the three-line budget holds. Nothing is
  said for a local run, for `--ref` other than `HEAD` (you chose what to ship), for a clean tree or one with
  only ignored files. Files are counted one by one (an untracked directory counts its files; a rename counts
  once), and nested repositories and `.claude/worktrees` are not counted. Only the run path counts them: hooks,
  `why` and `gate status` do not.
- **No git hook runs while shipping.** `git push` runs with `--no-verify`, so the repository's own `pre-push`
  never runs, whatever it prints or checks. The bare repo on the remote gets `core.hooksPath=/dev/null` in its
  own config each time a commit is shipped, so hooks of the remote machine (a global `core.hooksPath`, a
  `init.templateDir` that carries hooks) do not run in it either: not on the receiving side of the push, not in
  the `git worktree add` of the job. A hook that declines or prints can therefore neither fail a ship (exit
  77) nor add lines to a run's output.
- **The base ref travels too.** A command that diffs against `origin/main` (`git diff origin/main...`, a lint of
  the changed files) needs that ref, and a remote worktree has no remote called origin. So unless you say
  otherwise, heavyq recreates `refs/remotes/<base ref>` there, pointing at the same commit as here: the base ref
  is, in order, `--base-ref`, `queue.base_ref`, or origin's default branch (`refs/remotes/origin/HEAD`, else
  `origin/main`) when this repository has one. `--base-ref none` (or `queue.base_ref = "none"`) ships none. The
  commit is part of the [run id](gates.md#run-identity), so a cached result or a proof never outlives the base it
  saw, and a run here and a run there have the same id. A ref this repository does not have is skipped with a
  note, not an error. If a command still fails on a remote saying `origin/<branch>` is missing, the agent
  digest ends with `next: the remote has no origin/<branch>; re-run with --base-ref origin/<branch>` (for a gate
  command: set `queue.base_ref`), when this repository has that ref.
- **Environment variables don't travel.** `FOO=1 heavyq ... -- npm test` sets `FOO` only locally. Put them
  inside the command: `heavyq 3 -- env FOO=1 npm test`.
- **The same subdirectory.** If you run from `repo/packages/web`, the command runs in `packages/web` of the
  remote worktree.
- **Clean teardown.** The remote side holds a lifeline on stdin. If heavyq dies, even by SIGKILL, or the
  connection drops, the remote stops the command's process group and removes the worktree. Exit 255 means
  the connection was lost.

## An ordered list of machines

`--remote a,b,c` (also `HEAVYQ_REMOTE`, `[proof] remote`, and a gate's `remote`) names the machines a run may
use, in the order you prefer them. The run goes to the **first** machine of the list that has a free slot and
passes its RAM/load gate; a machine short of RAM, over its load limit or with every slot taken is passed over
(`status` says why, and "later in the --remote order" for a machine that had room but came later). Unlike
`auto`, the order is yours, not the placement score.

```toml
[proof]
remote = "mac,local,linuxi"   # the fast spare first, this machine while it has room, then the other spare
```

- Each entry is `local` or a configured `[[remote]]`, at least two, none twice. `auto`, empty entries and
  spaces are refused (exit 2 for `--remote`; a config error for the files).
- A list that names `local` acts like `auto` where this machine matters: with uncommitted changes a proof
  runs here, and `--needs`, `need_gh` and `--need-gh` narrow it to this machine. When a `--with` sibling or
  `--ref` makes this machine ineligible, the rest of the list is used.
- A list of remotes only acts like a named remote: the run never happens here, a proof of uncommitted code
  is refused, and the run exits 76 after `remote_down_max` if every remote in it is down.
- The anti-starvation bypass (`force_after`) applies only to a ticket with one candidate machine, so a list
  never pushes a run onto a machine over its load limit: it waits for whichever has room.

## Moving a waiting ticket: `heavyq retarget`

A queued ticket can change machines without leaving the queue: when the box it waits for is going away, when
another one has free slots, or when only another one has the tools the command needs.

```bash
heavyq retarget 48213 --remote spare         # by the pid shown in status, or by the ticket id
heavyq retarget 48213 --remote spare,local   # an ordered list, as for a run
heavyq retarget 48213 --remote local         # back to this machine
```

`WHERE` is what `--remote` takes on a run: `local`, `auto`, a `[[remote]]`, or a list. The ticket keeps its priority
and its place among equals, and from its next poll waits for the new machines as if it had been queued with
`--remote WHERE`:

- a remote runs the commit the ticket recorded when it was queued (with default placement, the commit checked out
  then), the same one it would have shipped to its first machine;
- this machine runs the work tree in place, as for any target that names `local`;
- the rules of the new target apply from then on: a ticket moved to remotes only exits 76 if they all stay
  unreachable for `remote_down_max`, and the anti-starvation bypass needs one candidate machine;
- the clocks of the old machines do not follow it: the new machines are probed once before the ticket can give up
  on them, and both `remote_down_max` and the head-of-line wait of the anti-starvation bypass count from the move. A
  ticket moved off an unreachable remote just before `remote_down_max` runs out is not dropped for it;
- a waiting ticket follows edits of the config file ([configuration.md](configuration.md#editing-the-config-while-tickets-wait)),
  and a move is checked with the config it follows: `heavyq retarget` (and the dashboard) files the request once the
  ticket decides with the file it read, within two polls, so a `[[remote]]` just added is a valid target. `auto`
  means the remotes of the config at the move, and the ticket keeps the machines it moved to across later edits, as
  it keeps those it was queued with: a remote added after the move is not one of them, and it never goes back to
  its old machines. A machine it moved to that an edit removes is `not in this config`, as for any candidate.

The ticket's own heavyq process re-plans its placement and answers, and `heavyq retarget` prints the answer: exit 0
once the ticket moved (or, when it has not read the request within two polls, it moves when it does), 2 when the
move is refused. A refused move changes nothing:

| Refused with exit 2 | Why |
|---|---|
| a name that is not `local`, `auto` or a configured `[[remote]]`, or a bad list | the checks of `--remote` |
| a running or finished ticket | only a waiting ticket moves |
| a ticket queued to run on this machine only, moved to anything but `local`: `--remote local`, or default placement that stayed here (uncommitted changes, a path outside the repository, no git repository), `--need-gh` | it recorded no commit a remote could run. Cancel it (`heavyq cancel`) and queue it again with `--remote WHERE` inside a git repository |
| a ticket with `--needs`, moved to anything but `local` | pool values belong to this machine |
| a target whose machines are all paused | a paused machine admits nothing new: `heavyq resume` it first. In a list or `auto`, paused machines are skipped until they resume, as for every ticket |
| a target whose machines all lack a tool the run needs (its argv[0] or `--tools`, see [placement by tools](#placed-where-its-tools-are---tools)) | the ticket's own process asks the new remotes and refuses it, naming the tools and each remote's `env_path`. In a list or `auto`, a remote that lacks one is skipped and `why` says so |
| `local` for a ticket this machine cannot run: a `--ref` that is not checked out here, a `--with` sibling off its pin | the ticket's own process refuses it and says why |

The dashboard's control API files the same request: `POST tickets/{id}/target` (see [dashboard.md](dashboard.md)).

## Sibling repositories: `--with`

A command that reads `../shared-schema` or `../core-lib` needs those repositories next to the main one. Declare
them, and every machine the run may go to gets them there:

```bash
heavyq 3 --with shared-schema --with core-lib@v2.1.0 -- make contract-test
heavyq 3 --repo ~/work/app --with ~/work/tools/linters@main -- make lint   # a path: its directory name is used
```

`REPO[@REF]` is a directory name next to the main repository (`--repo`, default the git root of the current
directory) or a path containing `/`. `REF` is a commit or branch; without it, `HEAD` as it is when you enqueue.
The ref is resolved to a commit **at enqueue**, so a run is pinned however long it waits. Anything wrong stops
the run with exit 2 before it is queued: the directory is missing or is not the root of a git repository, the ref
is unknown, the name has characters outside `A-Za-z0-9._-` (or is `.`/`..`, or starts with `.`/`-`), it is the main
repository, or two `--with` land on the same name.

- **Remote:** each sibling's commit is pushed the same way the main repository's is, and checked out as a directory
  next to the main worktree with the same name, so `../shared-schema` resolves. Both are removed with the worktree.
- **Local:** the command runs in place, so this machine is eligible only when every sibling lives at
  `../<name>` (next to the main repository), has `HEAD` equal to the pinned commit and a clean work tree, and
  `--ref`, if given, is the main repository's `HEAD`. It is checked again when the ticket gets a slot, because
  siblings change while a ticket waits. When local is not eligible, `--remote local` exits 2 naming the sibling
  that is off; `--remote auto` and default placement use the remotes only (one stderr line says why).
- **Identity:** each sibling's name and full commit are part of the run id, so the cache and `gate status` never
  take a result for one sibling commit as a result for another. `--cache` works with `--with`. A local result is
  not stored (and records no tree) if a sibling changed while the command ran.
- **Placement:** with `--with`, `../<name>` in the command does not by itself keep default placement local; any other
  path outside the repository still does.
- `heavyq status` shows `with: name@sha12` under the ticket, `heavyq history -v` under the run, and the ledger
  and `status --json` carry it as `with`.

## The GitHub CLI: `--need-gh`

A command that calls `gh` with your login works only on the machine that has it. `--need-gh` keeps the run on this
machine: the same as `--remote local`, and combined with `--remote <name>` it is a usage error (exit 2; `--remote
auto` just narrows to local). List repositories whose commands always need it in the user config:

```toml
[run]
need_gh_repos = ["app", "billing-api"]   # repository directory names
```

A run in one of them behaves as if `--need-gh` were given and prints one stderr line saying why. A `[[gate]]` can
say `need_gh = true` (see [gates](gates.md)).

## Placed where its tools are: `--tools`

A free slot is not enough: a remote whose PATH lacks `npx`, `envsubst` or `initdb` turns a run red for a reason that
has nothing to do with the code. Before a ticket is queued, heavyq asks each candidate remote whether the commands
the run needs resolve on the PATH a job gets there (the job script's own PATH code: the login shell's PATH on macOS,
then `env_path` first), and leaves out a remote that lacks one.

```bash
heavyq 3 --remote auto -- npx vitest run                  # npx is checked on every candidate remote
heavyq 3 --tools envsubst,initdb -- bash scripts/e2e.sh   # argv[0] is bash: name what the script calls
```

- **What is checked.** The command's argv[0], seen through `env` (`-i`, `NAME=VALUE`), `nice`/`nohup`/`time`/`exec`
  without options, a shell's `-c` script (its first command after leading assignments, read only up to the first
  quote, `$`, separator or redirect) and the wrapper `heavyq gate run` adds: `sh -c 'cd web && pnpm test'` checks
  `sh` and `cd`, not `pnpm`. A path (`./gradlew`, `/opt/x/bin/x`) is not checked. Name everything else in `--tools
  a,b` (repeatable) or in a gate's `tools = [...]` ([gates](gates.md)). Tool names are plain command names
  (`[A-Za-z0-9_][A-Za-z0-9._+-]{0,63}`, at most 32): a path, a space or shell syntax is exit 2.
- **Skipped, and said so.** A remote that lacks a tool is not a candidate of the ticket. `heavyq why <lane>` and
  `heavyq status` say `box: lacks envsubst (see its env_path)`, `status --json` has it in the ticket's `reasons`, and a
  person also gets one stderr line when the run is queued. `why`, `status` and the hook never probe: the reason is
  stored on the ticket.
- **Refused before shipping.** When no machine is left (`--remote box`, a list, or `auto` while this machine is not
  eligible), the run exits 2 before anything is pushed, naming each remote, the tools it lacks and its `env_path`.
  An agent gets that as the usual digest (`heavyq: NOT RUN ...`, the remotes and tools in the first lines, `next:`).
- **Cached.** Answers are kept per remote in `remote-tools/<remote>.json` in the state directory (beside the ledger):
  "found" for an hour, "missing" for two minutes; a changed `env_path` or transport starts over. Before refusing a run
  on a cached "missing", heavyq asks that remote again. A probe also asks about `go`, `node`, `npx`, `pnpm`,
  `python3`, `cargo` and `docker`, so most later runs need no probe. Only remotes the metrics probe reached are asked;
  a remote that cannot be asked keeps its place (unknown is not missing). A tool counts as found when it is a shell
  builtin or an executable file on that PATH, not when a file of that name is there without the execute bit.
- **Not known in advance.** A tool the `cache_hook` installs, or one in a relative `env_path` directory (resolved in
  the job's worktree, which a probe does not have: a tool missing there counts as unknown).
- **While it waits.** A ticket keeps what placement by tools found, except where that changes. A move (`heavyq
  retarget`) is placed by tools as a run is: a remote of the new target that lacks a tool is skipped (`why` says so),
  and a move that leaves no machine is refused by the ticket, naming the tools and `env_path`; it keeps its machines.
  An edit of the config that changes the `env_path` or transport of one of its remotes makes it ask again: a remote
  that now has the tool is a candidate again, one that lost it is skipped, and a ticket no machine is left for exits
  2, as it would have been refused when queued.
- **`heavyq doctor`** asks every remote now and lists which of those common tools, and of the tools the gates of the
  current repository's `heavyq.toml` declare, resolve there (`box: go, node | missing: pnpm`). Its answers refresh
  the cache.
- **Exit 127 on a remote** (the shell's "command not found") ends with one line naming the remote and its `env_path`
  (for an agent, the digest's `next:` line), and drops what was cached about that remote, so the next run asks again.

This machine is not checked: a local run uses your own PATH, where a missing command is the usual exit 127.

Remote requirements: `bash`, `git`, `base64`, `awk`, and either Linux with `flock` and `setsid` (util-linux) or
macOS (below). Nothing is installed. Everything lives under `work_dir` (default `~/.heavyq`).

## A spare Mac as a remote

A stock Mac works as a remote with nothing installed: no `flock`, no `setsid`, no Homebrew. The remote script
notices what is missing and uses Perl, which ships with macOS, instead. Linux remotes run exactly the code
they always did.

```toml
[[remote]]
name = "mac"
ssh = "mac"                              # an alias from ~/.ssh/config
heavy_slots = 2
env_path = "~/.local/go-sdk/bin"         # where the toolchain lives, if the login shell does not put it on PATH
```

```bash
heavyq status                            # probes the Mac: cores, load, available RAM, busy slots
heavyq 4 --remote mac -- go test ./...
```

How each Linux-only piece is replaced:

| Needed | Linux | macOS |
| --- | --- | --- |
| Slot lock | `flock -w 5` | Perl `flock()`: the same `flock(2)` on the same lock file, so a held slot looks busy to the probe and to the next job exactly as on Linux, and the kernel drops it when the job dies. A mkdir lock with a pid file would need stale-lock recovery; this does not. |
| Own process group | `setsid` | Perl `POSIX::setsid()` then `exec bash -c`: same pid, so the whole group is still stopped on teardown. |
| Teardown on lost connection | stdin lifeline | unchanged: EOF on stdin stops the group and removes the worktree. |
| Probe metrics | `/proc/meminfo`, `/proc/loadavg` | `sysctl` and `vm_stat`, parsed by the same code and formulas as the local macOS sampler (available = free + inactive + purgeable + speculative pages, capped at installed RAM). |

**PATH.** A non-interactive `ssh` command gets a bare PATH (`/usr/bin:/bin:/usr/sbin:/sbin`), so user-local
toolchains are not found. On macOS the job script asks your login shell (`zsh -l`, or `bash -l` if that is your
shell) for its PATH, so whatever `~/.zprofile` and `~/.bash_profile` set is visible to the command (a profile
that hangs is cut off after 10 s). `~/.zshrc` is not read by a login shell that is not interactive, so for
anything set only there use `env_path`: colon-separated directories put first on the command's PATH, with a
leading `~/` meaning the remote home. It works on Linux remotes too. Quotes, `$`, backticks and control
characters are refused when the config loads.

macOS ships bash 3.2, which runs the job script and the `cache_hook`: avoid bash 4 features (`mapfile`,
associative arrays) and GNU-only flags (`cp -al`, `mv -T`, `sha256sum`) in hooks. Requirements: `git` and Perl
(both present on a stock Mac; `git` comes with the Xcode command line tools).

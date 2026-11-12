# History as a ledger (commit, tree, session)

Each run appends one JSON line. These fields say what it ran on and who asked:

| Field | Meaning |
|---|---|
| `v`, `id` | schema (`2`) and a ULID: unique, and ordered by creation time at millisecond resolution only. Ids made in the same millisecond by different processes have no order, so order lines by their position in the file or by `end`. Lines without `v` are v1 and still load. |
| `repo` | hash of the git common dir: the same for every work tree of one repository |
| `worktree`, `subdir` | where the command ran |
| `commit` | `HEAD`. For a remote run, the commit that was shipped. |
| `tree` | the exact files. **Clean:** `HEAD`'s tree id, the same one the cache keys on. **Dirty:** `<HEAD tree>+<digest>`, where the digest covers `git diff HEAD --binary` (staged or not) and the names and contents of untracked files that aren't ignored. The same edits give the same id, any edit changes it, and reverting restores the clean id (also when the edits cancel out, like staged then reverted). **Absent:** the tree is unknown and never counts as proof (see below). |
| `dirty` | uncommitted or untracked files at the start (absent = false). Remote runs ship a commit, so they are never dirty. |
| `session`, `item`, `gate` | `--session`, `--item`, `--gate` (below) |
| `agent`, `model` | the coding agent (`claude-code/2.1.90`) and model (`claude-opus-5`) of the session, joined from its session meta record (`heavyq hook claude-sessionmeta`, see [flow.md](flow.md)) when the run is written. Absent without a session or a record |
| `cmd_hash` | sha256 of the exact argv, taken before `cmd` is redacted and cut to 200 bytes; so long gate commands can be proven |
| `run_id` | sha256 of the run identity: tree, exact argv, subdir, `cache.env` values and base ref commit. Cache, proof, flaky and loop detection compare it, so a run under another environment never proves a gate it did not run (see [docs/gates.md](gates.md)) |
| `load1`, `free_mb`, `cores`, `max_load`, `job_mb` | the machine when the run was admitted (local sample, or the probe a remote placement used) and the limits the resource gate applied; with `forced`, they tell a failure under load from a real one (see [docs/gates.md](gates.md)) |

- **Tags.** `--session ID`, `--item ID`, `--gate NAME`; `HEAVYQ_SESSION` and `HEAVYQ_ITEM` supply the first
  two when the flags are absent, and for `session` the Claude Code hook's file comes after them (see the
  hook section). Names are 1-80 characters of `A-Za-z0-9._:-`; anything else exits 2.
- **Dirty trees are identified, never cached.** `--cache` still skips them. Hashing the edits of a
  10,000-file repository takes about 35 ms (20 ms when clean).
- **A tree that isn't certain isn't claimed.** The line keeps `commit` and `dirty` but records no `tree`
  when: a local command edits the work tree it runs in; the tree changed between computing a cached or
  coalesced result's key and replaying it (a result proves the tree it was computed on); a submodule or an
  untracked nested repository has changes of its own (git only reports "dirty" for those, whatever they
  are); a tracked file is flagged assume-unchanged or skip-worktree, which a sparse checkout does to every
  path outside its cone (git then reports such a file as unchanged, whatever the work tree holds or lacks),
  in the repository or in any checked-out submodule;
  more than 256 MiB of untracked content would have to be read; or `HEAD` moved while the tree was
  being identified. Untracked files over 64 MiB are identified by size, mtime, inode and change time rather
  than content. A submodule that sits at another commit is fine: the commit is part of the id. A submodule
  that is not checked out (`git worktree add` never checks them out) makes the tree dirty, with an id of its
  own: a run there never proves a checkout that has the submodule's files. The same holds for a submodule
  of a checked-out submodule (`git submodule update --init` without `--recursive`).
- **Outside a repository**, the identity fields are simply empty.
- **Where.** `$HEAVYQ_HISTORY` if set (exactly that one file, never rotated). Otherwise one file per month,
  `history-YYYY-MM.jsonl` (UTC) in `$XDG_STATE_HOME/heavyq`. `heavyq history` and the dashboard read across
  the months, newest last, and still read the `history.jsonl` that older versions wrote.

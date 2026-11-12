# Exit codes

| Code | Meaning |
|---|---|
| *command's own* | the command ran; this is its exit code (128+n if killed by signal n) |
| 2 | usage or setup error (nothing ran); also a waiting ticket whose `--needs` no longer fit the pools after a config edit ([configuration](configuration.md#editing-the-config-while-tickets-wait)) |
| 75 | `--max-wait` elapsed before the ticket's turn (EX_TEMPFAIL: retry later) |
| 76 | the ticket can only run remotely and its remotes were unreachable for `remote_down_max` (a remote removed from the config while the ticket waits counts as unreachable, see [configuration](configuration.md#editing-the-config-while-tickets-wait)) |
| 77 | remote preparation failed (push, worktree, cache hook, or a slot taken outside the queue) |
| 78 | cancelled by an operator (`heavyq cancel` or the dashboard) |
| 127 / 126 | command not found / not executable. Also 127 under `--strict`, in gates and in item proofs when a run exited 0 but its output has a shell "command not found" line (see below) |
| 255 | connection to the remote lost mid-run (the remote side stops the command) |

The control commands exit 2 when they refuse a request, and nothing changes then: `heavyq cancel` and `reprio` for no
such ticket (or a running one, for `reprio`), `heavyq retarget` for a move it cannot make safely (a running ticket, an
unknown or paused machine, a ticket queued for this machine only; see
[remote.md](remote.md#moving-a-waiting-ticket-heavyq-retarget)).

## A 0 that is not trusted (`PASS?`)

`sh -c 'npx tsc | tail'` exits with `tail`'s status, so a missing `npx` still gives 0. When a run exits 0 and
its output has a line that is a shell's own not-found report (`sh: 1: X: not found`, `bash: line N: X: command
not found`, `bash: X: command not found`, `zsh: command not found: X`; the whole line must be that, so a test
log that only mentions "not found" does not count), heavyq does not call it a pass:

- the verdict is `PASS?` with one warning that quotes the line, and the exit code stays 0, so scripts keep working;
- the ledger line is `exit 0` with `result: "suspect"`: it never proves a gate or an item, and the result is not
  stored in the cache, so the next `--cache` run really runs;
- `--strict` makes it `FAIL` with exit 127. `heavyq gate run` and `heavyq item done --run` always are strict,
  because they produce proofs: the ledger line is exit 127, `result: "suspect"`.

The check sees what the command printed to a pipe or a file; a stream that is a terminal is left to the person
reading it. A `sh -c`/`bash -c` string with a `|` also draws one warning: the exit status is the last stage's.
Use `bash -o pipefail -c '...'` or avoid the pipe (heavyq keeps the full log).

`heavyq hook ...` commands are called by Claude Code, whose own contract applies: they exit 0 and print nothing on any doubt. The one exception is exit 2 from `heavyq hook claude-stop` for `TaskCompleted` and `TeammateIdle` in `destroy` mode, which is how Claude Code blocks those events (the reason is on stderr); see [steer.md](steer.md).

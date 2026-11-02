# Exit codes

| Code | Meaning |
|---|---|
| *command's own* | the command ran; this is its exit code (128+n if killed by signal n) |
| 2 | usage or setup error (nothing ran) |
| 75 | `--max-wait` elapsed before the ticket's turn (EX_TEMPFAIL: retry later) |
| 76 | the ticket can only run remotely and its remotes were unreachable for `remote_down_max` |
| 77 | remote preparation failed (push, worktree, cache hook, or a slot taken outside the queue) |
| 78 | cancelled by an operator (`heavyq cancel` or the dashboard) |
| 127 / 126 | command not found / not executable |
| 255 | connection to the remote lost mid-run (the remote side stops the command) |

`heavyq hook ...` commands are called by Claude Code, whose own contract applies: they exit 0 and print nothing on any doubt. The one exception is exit 2 from `heavyq hook claude-stop` for `TaskCompleted` and `TeammateIdle` in `destroy` mode, which is how Claude Code blocks those events (the reason is on stderr); see [steer.md](steer.md).

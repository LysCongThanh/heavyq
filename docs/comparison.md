# Comparison

| | heavyq | [pueue](https://github.com/Nukesor/pueue) | [task-spooler](https://viric.name/soft/ts/) | GNU parallel / `sem` | plain `flock` |
|---|---|---|---|---|---|
| Model | wrapper, no daemon; queue is a directory | daemon + client | per-user server | job runner; `sem` = counting semaphore | kernel lock |
| Priority | 1-9, FIFO within | yes (`--priority`, since 3.2) | urgent flag (`-u`) | no | no |
| Caller blocks until done, gets exit code | yes | no (`pueue wait` / `follow`) | optional (`-c`, `-w`) | yes | yes |
| Concurrency limits | heavy + light slots per machine | per-group parallelism | slots (`-S`) | `-j` / `sem -j` | 1 |
| RAM / load gate | yes, with reservation + anti-starvation | no | no | `--memfree`, `--load` (own jobs) | no |
| Light jobs jump ahead | yes, into light slots | no | no | no | no |
| CPU attribution per lane | yes (`status`) | no | no | no | no |
| Remote execution | by exact commit, temp worktree, auto placement | no (remote *clients*) | no | `--sshlogin` (copies files, no git) | no |
| Pause / resume / edit queued tasks | no | yes | partly | no | no |
| Persistent logs of each task | no (streams to caller) + history | yes | yes | `--results` | no |
| Agents use it without being told | yes (Claude Code hook rewrites heavy commands) | no | no | no | no |
| Identical runs share one result | yes (`--cache`: by git tree, in flight and cached) | no | no | no | no |

Pick **pueue** if you want a long-lived personal task manager with pause, edit and logs. It is excellent,
and heavyq owes the "queue for your shell" idea to it and to task-spooler. Pick **heavyq** when many
independent callers (agents, scripts, CI shells) each need to run something heavy, wait for their turn
in priority order without overloading the machine, and get the exit code back.

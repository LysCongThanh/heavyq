**`heavyq retarget`: move a waiting ticket to other machines.** `heavyq retarget <ticket|pid> --remote WHERE` (the
`WHERE` of a run's `--remote`: `local`, `auto`, a remote or an ordered list) changes the machines a queued ticket may
run on from its next poll, without cancelling and queueing it again: it keeps its priority and its place. A remote runs
the commit the ticket recorded when it was queued; this machine runs the work tree in place. The ticket's own process
re-plans and answers, so the command exits 0 once the ticket moved and 2 with the reason when the move is refused: an
unknown or paused machine, a running ticket, `--needs`, or a ticket queued for this machine only, which recorded no
commit a remote could run. The dashboard's control API files the same request (`POST /api/v1/tickets/{id}/target`).
See [docs/remote.md](docs/remote.md#moving-a-waiting-ticket-heavyq-retarget).

- **macOS: heavyq no longer deletes the tickets of live processes.** The process start token came from `ps -o lstart=`,
  which prints local time, so a `heavyq` started with another `TZ` (or any heavyq after the system time zone changed)
  saw every other ticket as a recycled pid and pruned it: waiters failed with "our ticket disappeared" and running jobs
  vanished from `status` and slot accounting. `ps` now always runs in UTC. A `ps` that fails or times out (a fork
  failure on an overloaded Mac) no longer counts as "process dead" either: the ticket, lease or coalesced run is kept
  until a check can tell. On a Mac whose zone is not UTC, tickets still held by an older heavyq
  during the upgrade are pruned once.
- **A heavyq that exited but was not reaped by its parent (a zombie) no longer holds its ticket.** Its waiting ticket
  stayed at the head of the queue and its running ticket kept a slot busy for as long as the parent lived; it is now
  pruned like any dead process, on Linux and macOS.

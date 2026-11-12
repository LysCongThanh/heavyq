- **A local command no longer runs on outside the queue.** SIGTERM, SIGHUP, SIGQUIT and every Ctrl-C of
  `gate run --parallel` now stop the command's whole process tree, not just its shell, and heavyq keeps the slot
  until all of it is gone, on macOS too when a grandchild outlived its shell and re-parented to launchd (SIGKILL after 10 s, or at once on a second signal, again on every pass). A local command
  now runs under a small lifeline process, so even if heavyq is killed outright (`kill -9`), the command's tree is
  stopped instead of sharing the freed slot and pool values with the next job, down to a grandchild that ignores
  SIGTERM (SIGKILL after 2 s, also on macOS where it was re-parented to launchd). Orphans of the command that exit
  are reaped as they go instead of piling up as zombies.
- **`heavyq ... -- cmd | head` no longer kills heavyq.** When the reader of heavyq's output went away, heavyq died
  of SIGPIPE: no ledger line, no digest, and the slot freed while the command ran on. It now keeps the command's
  output for its log and digest, supervises it to its end and records the run.
- **`gate run --parallel` commands have no terminal.** A command that prompts on `/dev/tty` (git credentials, a
  passphrase) was stopped in the background (SIGTTIN) and hung the gate with its slot held; it now fails at once.

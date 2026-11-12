- **A stopped or hung heavyq can no longer hang every waiter on the machine.** The queue lock was a blocking
  `flock`: a run stopped with Ctrl-Z (or SIGSTOP, or a debugger) inside a scheduling round kept it, and every other
  waiter blocked deaf to Ctrl-C, SIGTERM, `heavyq cancel` and `--max-wait`. Waiters now poll the lock and keep
  answering all four; a Ctrl-Z that lands in a round takes effect right after the lock is released; and pruning
  runs outside the lock.
- **A waiter suspended with Ctrl-Z no longer holds back the queue.** It runs no rounds, so it never claims, yet it
  stayed head of line for every machine it could use. It now keeps its ticket and place but does not count as a
  ticket ahead; `heavyq status` marks it `[stopped ...]`.
- **macOS: pruning runs `ps` once, not once per ticket.** Every waiter prunes every poll, so with N tickets the queue
  ran about N^2 `ps` per poll, serialized under the queue lock. A failed `ps` no longer prunes live tickets.
- **A wall clock stepped back no longer lets new tickets jump the queue.** A ticket is named after the enqueue time,
  and the name is the order: after a backward step (NTP, a VM resume, a manual date) new tickets sorted before older
  ones of the same priority. A new ticket is now named after the newest one in the queue when the clock is behind it.

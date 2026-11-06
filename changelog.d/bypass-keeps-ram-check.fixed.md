**The anti-starvation bypass no longer admits a job into a machine without the RAM for it.** After `force_after`, a
head ticket skips the load check only: its job's RAM (`job_mb + reserve_mb`, after the RAM held for just-started
jobs) must still fit, because a job without it can take down the jobs already running there. It cannot starve: the
head ticket keeps the machine for itself, our jobs there end, and an idle machine still admits. The bypass now applies
only to a ticket with one candidate machine; one that may run on several waits for whichever has room, so it is never
pushed onto a machine over its load limit. `status`, the story line and `top` say "skips the load check" and show no
countdown when the bypass would not help.

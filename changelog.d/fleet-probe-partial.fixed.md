**`heavyq fleet` on a loaded machine.** A machine at high load is no longer `unreachable: timed out` while ssh
answers: the probe starts a fixed handful of processes instead of several per busy process (on a loaded machine
each one can take half a second), sends the machine's numbers first, gives up on a command line read that hangs, starts no expensive step once half the
timeout is spent, and keeps what arrived when the timeout ends the transport. Such a machine shows its load,
RAM and slots with `partial: <what is missing> (<why>)` (`"partial"` in JSON). Zombie processes (exited, not
reaped, often by the thousand in a container without an init) are left out of process groups, counts and
attribution, in `heavyq fleet` and in the per-lane breakdowns of `heavyq status`.

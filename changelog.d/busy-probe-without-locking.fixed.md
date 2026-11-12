**Watching the queue no longer takes free slots.** `status`, `why`, `heavyq dash` and `top`, and the pool check in
front of every admission, used to find out whether a slot or pool value was free by locking it for an instant. A run
or a legacy `flock -n` user that tried the slot in that instant saw it busy: the run waited another poll, the
`flock -n` user failed. heavyq now asks the kernel who holds the file (`/proc/locks` on Linux, `fcntl(F_GETLK)` on
macOS) and takes the lock only when the kernel cannot tell, as in a container with its own pid namespace,
where `/proc/locks` leaves out holders outside it.

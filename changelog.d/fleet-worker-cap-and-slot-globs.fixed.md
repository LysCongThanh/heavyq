**`heavyq fleet`: the worker cap counts the slots in use, and foreign slot locks in a shared directory are
read.** The suggested worker cap divided the cores by every queue's configured heavy slots, so a remote where
heavyq is configured but ran nothing showed `cap 1`. It now counts per queue `max(busy now, most at once in the
window)`, at most the configured slots, falling back to the configured slots only for a queue without a
history (a foreign `history` that cannot be opened, or whose lines all fail to parse, is none, and the report
notes it), and shows every number with the result first (text puts each queue's term on a line of its own so the
count is never clipped; markdown; `worker_cap.queues` in `--json`). A foreign queue whose
slot locks are plain files beside other programs' (`/tmp/<queue>-N.lock`) showed `?` for its local slots; the
new `slot_globs = { <machine> = "/tmp/<queue>-[0-9]*.lock" }` names them, validated so it cannot match outside
its directory, and probed like `slot_dirs` (a symbolic link is never a slot). See [docs/fleet.md](docs/fleet.md).

- **A command that exits 0 while a background process keeps its output open now passes.** heavyq stops reading
  the output 10 s after the command exited, as before, but no longer turns that into exit 1 (a false FAIL in the
  ledger, the cache and gate proofs); it says why once.
- **A local exit 77 is the command's own failure.** 77 means "remote setup failed" only for a remote run; a local
  test script that exits 77 (the automake SKIP code) is now a FAIL, not "NOT RUN, the remote is unreachable", and
  its ledger line is a run, not "gave up".
- **A lost remote connection no longer counts as a failure of the code.** Exit 255 from a remote is recorded as
  `interrupted`, so it neither fails a gate nor makes a proven command "flaky". The remote job now remaps a
  command's own 255 to 96 (like 97 and 98), so 255 from a remote always means the connection.
- **`gate run` tells a crash from a stop.** A gate command that dies of its own signal (the OOM killer's 137, a
  segfault) or exits 78 itself is an ordinary failure: `--keep-going` and `--parallel` go on, and the gate ends
  "not proven" (exit 1) with its table. Only a signal or an operator cancel of heavyq itself is passed through.
  `item done --run` follows the same rule.
- **`gate run` really re-runs a command that failed under load.** With `cache_failures = true` the re-run it
  advises ("rerun once before debugging") used to replay the stored failure until the cache entry expired.
- **The cache never stores a result under another tree's key.** A tree edited during the queue wait and restored
  while the command ran had the key again at the end, and the edited tree's result was stored and later replayed as
  the committed tree's proof. This holds for a gate with `paths` as well, for an edit inside or outside its paths.
- **Default placement with `--ref` runs that ref.** An implicit `auto` ticket re-pinned HEAD right before going
  remote, so `heavyq 5 --ref v1 -- ...` ran and recorded HEAD instead of `v1`.

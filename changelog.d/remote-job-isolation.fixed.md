- **Each remote run has a checkout and a base ref of its own.** Runs of one repository on one remote shared
  `refs/remotes/<base_ref>`: a run started after a `git fetch` moved it under a run already going, and it could be
  left behind for later runs that asked for no base. A run whose remote side died without its cleanup (a reboot, a
  SIGKILL there) left its checkout under `work_dir` forever; the next job now reclaims it, and refs of shipped commits
  are dropped 30 days after their commit date.
- **A cancel or a dropped connection stops the `cache_hook` at once.** It used to run to its end (an `npm ci`) while
  the remote slot stayed locked, so the next ticket on that slot failed with 77.
- **A remote command runs as typed.** Its first word is a command name as it is locally (`time -v make` runs
  `/usr/bin/time`, `FOO=1 env` is not an assignment), and an argv over about 96 KB, which runs locally, no longer
  fails remotely with "argument list too long" (exit 77): the command line travels on the job's stdin.
- **A command's own exit 77 or 255 is its own failure.** A command that exits 77 (the automake/meson SKIP code) was
  recorded as gave up and digested as "NOT RUN: remote setup failed", locally too; a remote command's own 255 was
  reported as a lost connection and an agent was told to retry. 77 is now a plain `FAIL`, and a remote command's own
  255 is reported as 96 like 97 and 98. See [docs/exit-codes.md](docs/exit-codes.md).

**A remote that loses exit codes no longer proves anything.** A program named `setsid` that forked and exited 0,
first on a remote's PATH, made every run there pass (`false` included) and cached false proofs. The remote job
script now uses `setsid` only when it is util-linux's (else Perl), and before a remote may prove anything heavyq
runs an exit canary through the same job path: `exit 3` must come back as 3, after its command finished. The
verdict is cached per machine in the state directory (a pass for 24 h, a failure for 1 h). On a remote that fails
it, gates, item proofs, `--cache` and `--strict` runs are refused with exit 77; other runs are refused too unless
the remote sets `allow_untrusted_exit = true`, and then they are recorded as suspect. `heavyq why`, `status` and
`doctor` say "exit codes not trustworthy on <machine>". See [docs/remote.md](docs/remote.md).

**A remote run gets origin's default branch as its base ref.** A command that diffs against `origin/main` failed on the
first remote run of a repository, because the temporary worktree there had no `refs/remotes/origin/main`, and only
`--base-ref origin/main` or `queue.base_ref` fixed it. Now, when neither is set and the repository has
`refs/remotes/origin/HEAD` (else `origin/main`), heavyq recreates that ref on the remote machine, pointing at the same
commit. `--base-ref none` (or `queue.base_ref = "none"`) opts out, and a repository without origin ships nothing. The
base commit was already part of the run id, so it now includes origin's default branch wherever the run executes: a run
here and a run there keep the same id, `gate run` and `gate status` agree, and a cached result or a proof is a miss
again after a `git fetch` moves that branch (set `queue.base_ref = "none"` if your commands never look at it). When a
remote command fails saying an `origin/<branch>` is missing, the agent digest now suggests `--base-ref origin/<branch>`.
See [docs/remote.md](docs/remote.md).

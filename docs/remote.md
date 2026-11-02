# Spare machines

Add a remote in `~/.config/heavyq/config.toml`:

```toml
[[remote]]
name = "buildbox"     # used in --remote buildbox and in status
ssh = "buildbox"      # an alias from ~/.ssh/config (key auth; ControlMaster recommended)
heavy_slots = 2
light_slots = 2
cache_hook = '''
# optional: restore node_modules from a cache keyed by the lockfile
[ -f package-lock.json ] || exit 0
key=$(sha256sum package-lock.json | cut -c1-16); c="$HEAVYQ_CACHE_DIR/nm-$key"
if [ -d "$c" ]; then cp -al "$c" node_modules; exit; fi
npm ci --no-audit --no-fund && { cp -al node_modules "$c.$$" && mv -T "$c.$$" "$c" || rm -rf "$c.$$"; } 2>/dev/null; true
'''
```

Then:

```bash
heavyq 3 --remote buildbox -- npm test        # always there
heavyq 3 --remote auto -- npm test            # best score; you vouch the command is self-contained
heavyq 3 -- npm test                          # best score only if the repo is clean and paths stay inside it
heavyq 3 --remote local -- npm test           # never leave this machine
heavyq item done login-api --run --remote auto   # a proof placed like any other run; or set [proof] remote once
heavyq 3 --needs-ram -- npm run e2e           # place by free RAM only
heavyq 3 --remote buildbox --base-ref origin/main -- npm run lint:changed   # recreate origin/main there
heavyq 3 --remote buildbox --with ../shared-lib@v2.1.0 -- npm test          # sibling repo pinned to a commit
```

What a remote run sees, and what it doesn't:

- **Only committed code.** The exact SHA of `--ref` is pushed with `git push` (it doesn't need to be on
  GitHub). Uncommitted edits and ignored files (`.env.test`, `dist/`) stay behind.
- **Environment variables don't travel.** `FOO=1 heavyq ... -- npm test` sets `FOO` only locally. Put them
  inside the command: `heavyq 3 -- env FOO=1 npm test`.
- **The same subdirectory.** If you run from `repo/packages/web`, the command runs in `packages/web` of the
  remote worktree.
- **Clean teardown.** The remote side holds a lifeline on stdin. If heavyq dies, even by SIGKILL, or the
  connection drops, the remote stops the command's process group and removes the worktree. Exit 255 means
  the connection was lost.

Remote requirements: `bash`, `git`, `base64`, `awk`, and either Linux with `flock` and `setsid` (util-linux) or
macOS (below). Nothing is installed. Everything lives under `work_dir` (default `~/.heavyq`).

## A spare Mac as a remote

A stock Mac works as a remote with nothing installed: no `flock`, no `setsid`, no Homebrew. The remote script
notices what is missing and uses Perl, which ships with macOS, instead. Linux remotes run exactly the code
they always did.

```toml
[[remote]]
name = "mac"
ssh = "mac"                              # an alias from ~/.ssh/config
heavy_slots = 2
env_path = "~/.local/go-sdk/bin"         # where the toolchain lives, if the login shell does not put it on PATH
```

```bash
heavyq status                            # probes the Mac: cores, load, available RAM, busy slots
heavyq 4 --remote mac -- go test ./...
```

How each Linux-only piece is replaced:

| Needed | Linux | macOS |
| --- | --- | --- |
| Slot lock | `flock -w 5` | Perl `flock()`: the same `flock(2)` on the same lock file, so a held slot looks busy to the probe and to the next job exactly as on Linux, and the kernel drops it when the job dies. A mkdir lock with a pid file would need stale-lock recovery; this does not. |
| Own process group | `setsid` | Perl `POSIX::setsid()` then `exec bash -c`: same pid, so the whole group is still stopped on teardown. |
| Teardown on lost connection | stdin lifeline | unchanged: EOF on stdin stops the group and removes the worktree. |
| Probe metrics | `/proc/meminfo`, `/proc/loadavg` | `sysctl` and `vm_stat`, parsed by the same code and formulas as the local macOS sampler (available = free + inactive + purgeable + speculative pages, capped at installed RAM). |

**PATH.** A non-interactive `ssh` command gets a bare PATH (`/usr/bin:/bin:/usr/sbin:/sbin`), so user-local
toolchains are not found. On macOS the job script asks your login shell (`zsh -l`, or `bash -l` if that is your
shell) for its PATH, so whatever `~/.zprofile` and `~/.bash_profile` set is visible to the command (a profile
that hangs is cut off after 10 s). `~/.zshrc` is not read by a login shell that is not interactive, so for
anything set only there use `env_path`: colon-separated directories put first on the command's PATH, with a
leading `~/` meaning the remote home. It works on Linux remotes too. Quotes, `$`, backticks and control
characters are refused when the config loads.

macOS ships bash 3.2, which runs the job script and the `cache_hook`: avoid bash 4 features (`mapfile`,
associative arrays) and GNU-only flags (`cp -al`, `mv -T`, `sha256sum`) in hooks. Requirements: `git` and Perl
(both present on a stock Mac; `git` comes with the Xcode command line tools).

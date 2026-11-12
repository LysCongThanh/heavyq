- **Shipping a commit to a remote no longer fails, leaks or hangs.** Several runs of one new commit admitted to one
  remote together (`gate run --parallel`, two agents) all ship it, also the first runs of a repo on a remote; the
  slower ones used to exit 77 with "reference already exists" or "could not lock config file". Your push settings no longer apply to the ship: `push.recurseSubmodules=on-demand` used to push a
  submodule's unpushed branch to its real origin, and `push.gpgSign` or `push.pushOption` made every remote run exit 77.
  The push's ssh gets `BatchMode`, `ConnectTimeout` and keepalives (unless you chose an ssh for git), and Ctrl-C,
  SIGTERM and `heavyq cancel` now stop a push at once (130/143 or 78) instead of being ignored until it ended, with
  the slot held. See [docs/remote.md](docs/remote.md).
- **A SHA-256 repository or a shallow clone stays on this machine.** Default placement used to send it to a remote by
  score, where the ship can only fail (exit 77); a remote-only target is now refused up front (exit 2).

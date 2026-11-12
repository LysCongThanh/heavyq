- **The user config refuses values that loaded but could not work.** Each is now a load error that names the key:
  - a `git_url` that starts with `-` or holds control characters (`git push` read it as an option), and an `ssh`
    that is not a plain host alias when `transport` is customised (it still builds the default `git_url`).
  - `max_load_ratio = nan` and `mentor.min_lift_pp = nan` (TOML has `nan`, and every range check let it through):
    the load gate was silently off, `status --json` and history records failed to encode, and every mentor rule
    muted itself.
  - a `[[remote]]` named `auto`: `--remote auto` and `[proof] remote = "auto"` meant the scheduler, so the machine
    could never be targeted by its name.
  - a `hook.binary` with arguments, a relative path or a `~` other than a leading `~/`: the hook writes it quoted as
    one word, so every rewritten agent command exited 127. A leading `~/` is now expanded to your home.
  - a negative duration (`"-5m"`) or `load_weight_mb`, which silently became the default, and a `queue.poll` under
    `50ms`, which turned every waiting ticket into a busy loop on the queue lock. `0` still means the default.
  - `legacy_lock_files` entries that are relative (each repository locked its own `<repo>/build-1.lock`, so the
    shared limit and the exclusion with `flock` users were gone), empty, listed twice (two slots that admit one) or
    sharing a base name (two slots with one name in `status` and claims).

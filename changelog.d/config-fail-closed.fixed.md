- **The user config refuses values that loaded but could not work.** Each is now a load error that names the key:
  - a `git_url` that starts with `-` or holds control characters (`git push` read it as an option), and an `ssh`
    that is not a plain host alias when `transport` is customised (it still builds the default `git_url`).

**`heavyq statusline --wrap` no longer leaves your command's children running after a slow refresh.** Past its
5 s limit the wrapped command was killed but not what it started, so a script whose `git status` or `npx` was stuck
left one more stray process behind on every refresh. The command now runs in its own process group, and the
timeout kills the whole group; a command that finishes in time is untouched.

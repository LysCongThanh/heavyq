**`work_dir = "~/..."` on a remote works.** The remote scripts read it as a directory literally named `~` under the
home, while `git push` to the default `git_url` expanded `~/` to the home, so the bare repo and the push went to two
different places and every run on that remote exited 77 (status still called it healthy). A leading `~/` now means
the remote home, as for `env_path`; any other `~` is refused at load. The docs wrongly gave the default as
`~/.heavyq`: it is `.heavyq`, relative to the remote home.

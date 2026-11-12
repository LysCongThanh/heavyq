**A waiting ticket follows edits of the config file.** A ticket waiting for its turn decided with the config it
was submitted with, so a slot or a higher `max_load_ratio` added while it waited was never used, while `heavyq
status` and `why` (fresh processes reading the new file) said it "starts on the next poll", and every later ticket
waited behind it in turn order. A waiting ticket now re-reads the file when it changes (one `stat` per poll),
uses an edit once two polls in a row read it the same (never a half-written file), keeps its last good config and says so once when an edit does not load (a vanished file included, never the
defaults), adopts an edited `probe_timeout`, and gives up with exit 2 when an edit
removes a pool it needs. Its candidate machines stay those it was submitted with (a remote added later is not one
of them, `--with` and `--ref` included); a candidate remote removed from the file is `not in this config` in status
and counts as unreachable (exit 76 after `remote_down_max`). An edit refused because the repository's
`heavyq.toml` does not load is retried on every poll. `top` and the dashboard follow the file too (snapshots, probe
timeout, the machines `top --control` may pause), and status no longer promises a start to a ticket that decides
under another config. See [docs/configuration.md](docs/configuration.md#editing-the-config-while-tickets-wait).

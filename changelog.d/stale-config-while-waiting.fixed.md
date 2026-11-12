**A waiting ticket follows edits of the config file.** A ticket waiting for its turn decided with the config it
was submitted with, so a slot or a higher `max_load_ratio` added while it waited was never used, while `heavyq
status` and `why` (fresh processes reading the new file) said it "starts on the next poll", and every later ticket
waited behind it in turn order. A waiting ticket now re-reads the file when it changes (one `stat` per poll),
keeps its last good config and says so once when an edit does not load (a vanished file included, never the
defaults), adopts an edited `probe_timeout`, and gives up with exit 2 when an edit
removes a pool it needs. `top` and the dashboard follow the file too, and status no longer promises a start to a
ticket that decides under another config. See [docs/configuration.md](docs/configuration.md#editing-the-config-while-tickets-wait).

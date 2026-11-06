**An ordered list of machines for `--remote`.** `--remote mac,local,linuxi` (also `HEAVYQ_REMOTE`, `[proof] remote`
and a gate's `remote`) places the run on the first machine of the list that has a free slot and passes its RAM/load
gate, in the order you prefer, instead of pinning one box or letting the placement score choose. A box short of RAM
is passed over, not waited for. A list that names `local` behaves like `auto` for uncommitted changes, `needs` and
`need_gh`; a list of remotes only behaves like a named remote. See
[docs/remote.md](docs/remote.md#an-ordered-list-of-machines).

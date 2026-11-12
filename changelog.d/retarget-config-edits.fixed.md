**`heavyq retarget` and config edits agree.** A move to a `[[remote]]` added to the config a moment before was
refused as an unknown machine, because the waiting ticket had not yet adopted the edit it is checked with:
`heavyq retarget` and the dashboard now file the move once the ticket decides with the config they read (within
two polls). A ticket moved to `auto` waits for the remotes of the config at the move and, like any waiting ticket,
keeps the machines it moved to across later edits; a machine it moved to that an edit removes counts as down
(`not in this config`, exit 76 after `remote_down_max` for a remote-only ticket).

**Placement by tools holds while a ticket waits.** A move by `heavyq retarget` skipped the tools check, so a ticket
could be moved to a remote that lacks its command and fail there with 127: the ticket now asks the new remotes, skips
those that lack a tool (`why` says so) and refuses a move none is left for, naming the tools and `env_path`. An edit
that changes a candidate remote's `env_path` or transport makes a waiting ticket ask again instead of trusting what
the old PATH had; when none of its machines has the tools any more it exits 2, as it would have when queued.

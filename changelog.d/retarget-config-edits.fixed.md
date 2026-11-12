**`heavyq retarget` and config edits agree.** A move to a `[[remote]]` added to the config a moment before was
refused as an unknown machine, because the waiting ticket had not yet adopted the edit it is checked with:
`heavyq retarget` and the dashboard now file the move once the ticket decides with the config they read (within
two polls). A ticket moved to `auto` waits for the remotes of the config at the move and, like any waiting ticket,
keeps the machines it moved to across later edits; a machine it moved to that an edit removes counts as down
(`not in this config`, exit 76 after `remote_down_max` for a remote-only ticket).

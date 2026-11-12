**A pool can no longer be named after a variable heavyq reads.** A pool exports `HEAVYQ_<NAME>` into its command, so a
pool called `history`, `flow`, `steer`, `mentor`, `notify`, `stances`, `test` (and a few more) used to load and then
reach every heavyq the command ran: `history` moved their ledger, `flow = "off"` stopped flow refresh. Such a config
is now refused with exit 2, like `lane` or `slot` already were. A test keeps the reserved list in step with every
`HEAVYQ_` variable in the repository.

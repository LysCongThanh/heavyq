**Setting only `light_slots` in `[local]` no longer drops every heavy slot.** The documented defaults (2 heavy, 1
light) applied only when both keys were absent, so `[local] light_slots = 2` left this machine with no heavy slot:
every plain run, gate and hook-rewritten command waited forever (or exited 75 after `--max-wait`), and `doctor` said
the config was fine. Each absent key now gets its own default, and a config where no machine has a heavy slot is a
load error. `heavy_slots = 4` alone now also keeps the default light slot. Remote defaults (2 heavy, 0 light when
neither key is set) are unchanged and now documented.

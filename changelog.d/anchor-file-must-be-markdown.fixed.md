**`[anchor] file` can no longer take code out of the tree id.** The anchor file is left out of every tree id, with
everything below it, so a directory there (`file = "src"`, a plan directory that also holds scripts) made every
change under it invisible to proofs and the result cache. `heavyq.toml` now rejects an anchor file that is not one
`.md` file (a directory, a path ending in `/`, `.`, a code file), and the tree id falls back to the default
`.heavyq/anchor.md` whenever the configured path is a directory in the work tree or in the commit being identified.

**The user config schema refuses what heavyq refuses.** `schema/config.schema.json` now carries the validators' ranges
and name shapes (slots, `max_load_ratio`, RAM budgets, `[mentor]` percentages, `[cache]` sizes and `env` names, the
`[[hook.rule]]` `lane` and `match` words, `hook.binary`, `queue.base_ref`, a `[[remote]]`'s required `name`, ssh
alias, `work_dir` and `env_path`), so an editor or `taplo lint` flags a value before every heavyq command refuses the
config. `[local]` no longer offers the remote-only keys. The schemas, `docs/gates.md` and `examples/config.toml` also
caught up with the code: an ordered list `"a,b,c"` is a valid `remote`, `[anchor] file` is read and defaults to
`.heavyq/anchor.md`, a remote's slots default to 2 heavy and 0 light, and the example shows `[run]` and `[mentor]`.

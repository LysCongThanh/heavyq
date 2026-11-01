# Stances

A **stance** is a short document that describes a way of thinking: "stop polishing, rethink from the
problem", "pick one option and prove it", "do not change code for noise". The idea is that heavyq will switch
an agent into a stance when the evidence says it is stuck (a long time, many runs, no proof, the same failure
class again and again), then check whether it helped.

**What exists today** is the pure building block: the file format, the loader and a compact renderer, plus a
read-only command to look at them. **Triggers and injection come next** (evaluating the evidence, lane state,
hook wiring, the dashboard). Nothing in heavyq changes an agent's behaviour yet, and `heavyq stance` never
modifies anything.

```
heavyq stance ls [--json]
heavyq stance show <name> [--json]
```

- `ls` lists name, source and description. For repo and user stances the source includes the file path.
- `show` prints the full body (the text for humans). With `--json` it prints the whole stance, frontmatter
  included.
- An invalid stance file makes both commands exit 2 and name every bad file with its line. heavyq never falls
  back to the built-in stance an override was meant to replace.

## File format

A stance is a UTF-8 markdown file, `<name>.md`, with **TOML frontmatter** between two `+++` lines.

```markdown
+++
name = "breakthrough"             # optional; must equal the file name without .md
description = "One line for `stance ls`."
next = "write your from-scratch answer before reading the current design"
cooldown_minutes = 60
inject_tokens = 150               # default 150, 80-400

[enter]                           # evidence that should switch a lane into the stance
same_failure_class_across_trees = 3
churn_reverts = 2
keywords = ["breakthrough", "think differently", "đột phá"]

[exit]
decision_record = true
timebox_minutes = 30
+++
The stance text, in markdown. Any language.
<!-- core -->
- The (at most three) lines that are injected. Short.
<!-- /core -->
More text for `heavyq stance show`.
```

Why TOML and not YAML: heavyq already depends on `BurntSushi/toml` (no new dependency), TOML is strictly
typed, and its decoder lists every key it did not recognise, so a typo such as `cooldown_minute` is an error
with a line number instead of a setting that is silently ignored.

| Key | Meaning |
|---|---|
| `name` | `[a-z0-9][a-z0-9_-]{0,39}`; defaults to the file name; a different value is an error |
| `description` | one line, at most 160 characters |
| `next` | the one command or check the injection leads with, one line, at most 160 characters; optional |
| `enter.stall` | `minutes` and `runs` (at least one above 0) and `without_proof`. **Status line and dashboard only**: elapsed time alone is not evidence, so no built-in enters on it and triggers must not act on it alone. The key stays parseable |
| `enter.same_failure_class_across_trees` | the same failure class on this many different trees |
| `enter.churn_reverts` | reverts or rewrites of the same files without proof |
| `enter.keywords` | at most 20 phrases (one line, 80 characters each) that mean "the user asks for this stance" |
| `exit.decision_record` | the stance ends when a decision record exists |
| `exit.timebox_minutes` | or after this many minutes |
| `cooldown_minutes` | do not re-enter within this time |
| `inject_tokens` | budget of the compact form, see below; default 150, 80-400 |

Limits: the body is at most 32 KiB and must not be empty, the frontmatter at most 8 KiB. Over the limit is
an error, never a silent truncation. Unknown keys, wrong types and out-of-range numbers are errors of the
form `path:line: message`. A UTF-8 BOM and CRLF line endings are accepted. The `enter` and `exit` values are
validated and carried, but nothing evaluates them yet.

## Where stances come from

Highest precedence first; a stance with the same `name` in a higher layer replaces the lower one entirely.

1. **Repo**: `.heavyq/stances/*.md` under the git top level of the current directory.
2. **User**: `$XDG_CONFIG_HOME/heavyq/stances/*.md` (an absolute path), else `~/.config/heavyq/stances/*.md`.
3. **Built-in**: embedded in the binary.

Only regular `*.md` files are read; dot files and other extensions are ignored. Every other `*.md` file in
those directories has to be a valid stance, so a `README.md` there is an error: its name is not a valid
stance name. Drop a personal prompt in as is, in Vietnamese or any language, and it overrides the built-in one
of the same name.

## Built-in stances

All five are short English texts (each body at most 1.5 KiB), each with a `next` line and a core block.
`breakthrough` enters on deterministic evidence (the same failure class on 3 different trees, churn without
proof) or a keyword; the others carry keywords for now, their evidence triggers come with the trigger rules.
None enters on elapsed time alone.

| Stance | For |
|---|---|
| `breakthrough` | stuck on one approach: answer "from scratch, what is best?" before looking at the current one, at least three options including the status quo and a radical one, real versus inertia constraints, mandatory outside research, a steelman and a pre-mortem, cheap reversible experiments, a decision by measurable value with a migration path, trade-offs stated for the human |
| `converge` | exploring too long: pick one option, make the smallest provable change, run the gate |
| `skeptic` | about to claim done: prove it on this exact code and read the result |
| `calm` | the failure looks like noise: do not change code for it, re-run once, look at load |
| `surgeon` | the diff outgrew the task: cut it back to the minimal change |

## Compact form

`stance.Render(s, why)` produces the text that will be injected, **facts first**: the evidence, one thing to
do, a few core lines, and a pointer to the rest. At most `inject_tokens` x 4 bytes (a rough 4 bytes per
token), always valid UTF-8.

```
[heavyq · stance breakthrough] why now: same failure class on 3 trees: TestLogin timeout
next: write your from-scratch answer before reading the current design
- Answer "from scratch, what is best?" before looking at the current approach.
- List 3+ options: status quo, an incremental fix, one radical.
- Tell REAL constraints from INERTIA; research outside the repo is mandatory.
full: heavyq stance show breakthrough
```

- `next:` appears only when the stance sets `next`. The evidence in `why` is flattened to one line and limited
  to a third of the budget, `next` to a quarter.
- Core lines: at most 3. They come from `<!-- core -->` ... `<!-- /core -->` blocks in the body (blank lines
  skipped; unclosed, nested or stray markers are a parse error). Without a block, the body's first list items
  are used, else its first prose lines. Headings and the markers themselves are never injected.
- A line that does not fit the budget is skipped, never cut. If not even one fits (a single long paragraph),
  the first line is cut with an ellipsis.

## Roadmap

This is step S1 of the stances plan. Next: trigger rules as pure functions over the ledger, leases and git
churn; lane stance state and `heavyq stance enter|exit|off|stats`; hook wiring (inject on trigger, re-inject
after compaction, exit check); dashboard chips and the measurement view.

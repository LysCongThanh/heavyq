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
cooldown_minutes = 60
inject_tokens = 250               # default 250, 50-600

[enter]                           # evidence that should switch a lane into the stance
same_failure_class_across_trees = 3
churn_reverts = 2
keywords = ["breakthrough", "think differently", "đột phá"]

[enter.stall]
minutes = 40
runs = 8
without_proof = true

[exit]
decision_record = true
timebox_minutes = 30
+++
The stance text, in markdown. Any language.
```

Why TOML and not YAML: heavyq already depends on `BurntSushi/toml` (no new dependency), TOML is strictly
typed, and its decoder lists every key it did not recognise, so a typo such as `cooldown_minute` is an error
with a line number instead of a setting that is silently ignored.

| Key | Meaning |
|---|---|
| `name` | `[a-z0-9][a-z0-9_-]{0,39}`; defaults to the file name; a different value is an error |
| `description` | one line, at most 160 characters |
| `enter.stall` | `minutes` and `runs` (at least one above 0) and `without_proof`: claimed that long with that many runs and no proof |
| `enter.same_failure_class_across_trees` | the same failure class on this many different trees |
| `enter.churn_reverts` | reverts or rewrites of the same files without proof |
| `enter.keywords` | at most 20 phrases (one line, 80 characters each) that mean "the user asks for this stance" |
| `exit.decision_record` | the stance ends when a decision record exists |
| `exit.timebox_minutes` | or after this many minutes |
| `cooldown_minutes` | do not re-enter within this time |
| `inject_tokens` | budget of the compact form, see below |

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

All five are short English texts (each body at most 1.5 KiB). Today they carry keyword triggers; evidence
triggers for them come with the trigger rules.

| Stance | For |
|---|---|
| `breakthrough` | stuck on one approach: answer "from scratch, what is best?" before looking at the current one, at least three options including the status quo and a radical one, real versus inertia constraints, mandatory outside research, a steelman and a pre-mortem, cheap reversible experiments, a decision by measurable value with a migration path, trade-offs stated for the human |
| `converge` | exploring too long: pick one option, make the smallest provable change, run the gate |
| `skeptic` | about to claim done: prove it on this exact code and read the result |
| `calm` | the failure looks like noise: do not change code for it, re-run once, look at load |
| `surgeon` | the diff outgrew the task: cut it back to the minimal change |

## Compact form

`stance.Render(s, why)` produces the text that will be injected: at most `inject_tokens` x 4 bytes (a rough
4 bytes per token), always valid UTF-8.

```
heavyq stance: breakthrough — why now: 45 min, 11 runs, no proof on login-api
<the most important body lines>
full text: heavyq stance show breakthrough
```

- The evidence in `why` is flattened to one line and limited to a third of the budget.
- Body lines are chosen by importance: list items first, then headings, then prose; the chosen lines keep
  their order. A line that does not fit is skipped, never cut. If not even one line fits (a single long
  paragraph), the first line is cut with an ellipsis. Write stances so their list items carry the core.

## Roadmap

This is step S1 of the stances plan. Next: trigger rules as pure functions over the ledger, leases and git
churn; lane stance state and `heavyq stance enter|exit|off|stats`; hook wiring (inject on trigger, re-inject
after compaction, exit check); dashboard chips and the measurement view.

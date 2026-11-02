# Stances

A **stance** is a short document that describes a way of thinking: "stop polishing, rethink from the
problem", "pick one option and prove it", "do not change code for noise". heavyq switches
an agent into a stance when the evidence says the approach is not working (the same failure class again and
again, churn without a proof) or when the user asks for it, then checks whether it helped.

**What exists today:** the file format, the loader and a compact renderer; the evidence triggers (pure
rules over the ledger index and the repository history, plus the user's keywords); a lane's stance state with
cooldown and a cap; the exit check (a decision record, or a time box); a measurement of whether it helped; trust
for a repository's own stances; and one hook, `heavyq hook claude-userpromptsubmit`, that enters a stance and
injects it once. The SessionStart brief re-injects the active stance after compaction. The exit check at Stop and a PostToolUse
trigger are not wired yet: they can call the functions documented below.

```
heavyq stance ls [--json]
heavyq stance show <name> [--json]
heavyq stance enter <name> [--item ID] [--why TEXT]
heavyq stance exit [--item ID] [--force]
heavyq stance off
heavyq stance status [--json]
heavyq stance stats [--json]
heavyq trust [--list | --revoke]
```

- `ls` lists name, source and description. For repo and user stances the source includes the file path, and
  a repo stance that is not trusted says `(untrusted: run heavyq trust)`.
- `show` prints the full body (the text for humans). With `--json` it prints the whole stance, frontmatter
  included.
- An invalid stance file makes `ls` and `show` exit 2 and name every bad file with its line. heavyq never falls
  back to the built-in stance an override was meant to replace.
- `enter`, `exit`, `off`, `status` and `stats` act on the **lane of the current directory** (`$HEAVYQ_LANE`, else
  the name of the git work tree), so they are described under "Lane state" below.

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
validated, then evaluated as described under "Triggers" and "Exit and the decision record".

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
proof) or a keyword; the others carry keywords only (their evidence rules are future work). None enters on elapsed
time alone.

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

## Triggers

A trigger is a pure function of facts. **Elapsed time alone never enters a stance**: `enter.stall` stays
parseable for the status line and the dashboard, and `Evaluate` ignores it.

| Rule | Evidence | Read from |
|---|---|---|
| `enter.same_failure_class_across_trees = N` | the same command failing with the same exit code on N different trees of the lane, with no pass of that command since | the lane's file of the ledger index (`ledgerindex.LaneRuns`, 50 records; one small file) |
| `enter.churn_reverts = N` | N churn events in the commits since the lane's last proof | the work tree's commit log (50 commits at most; the window is the last 24 hours when the lane has never passed) |
| `enter.keywords` | one of the phrases is in the user's prompt | the UserPromptSubmit payload |

- A **failure class** is (repository, command, exit code). Runs that gave up, were cancelled, replayed a cache
  entry or failed under load (the machine explains them) do not count. Unknown trees do not count.
- A **churn event** is a revert commit (subject starts with `Revert `) or a non-revert commit that changes
  exactly the same set of files as an earlier commit in the window (the same code redone). A passing run
  of the lane ends the window, so an edit, run, commit loop never counts.
- A **keyword** matches case-insensitively on whole words in any script ("đột phá" matches "ĐỘT PHÁ!", not
  "đột phá2"). Only the first 16 KiB of the prompt is read. A decomposed (NFD) spelling of an accented keyword
  does not match its precomposed form: the standard library ships no normalisation tables.
- Order when several match: a keyword before any evidence, then a failure class, then churn, by name.
  Keywords are explicit intent: no holdout applies to them.

`stance.Evaluate(stances, Evidence)` is the pure core; `Env.CollectEvidence` reads only the evidence the
enter rules actually use.

## Lane state

One small file per lane and an append-only events log, under the git common dir (shared by every work tree,
never committed):

```
<git-common-dir>/heavyq/stance/lanes/<lane>.json   state: active stance, why, since, expiry, counts, cooldowns
<git-common-dir>/heavyq/stance/events.jsonl        enter | exit | off, with trigger, why, item and exit reason
```

- **One active stance per lane.** The state change is announced once, on entry. Evidence never interrupts a lane
  that is already in a stance; a keyword may switch it (the old one is logged as `replaced`).
- **Cooldown:** a stance is not re-entered by a keyword or evidence within its `cooldown_minutes` of the last
  time the lane entered or left it. **Cap:** at most 3 activations of one stance per session. `heavyq stance
  enter` is explicit and ignores both (and the mute).
- **`stance off`** leaves the stance and mutes the lane until the next `stance enter`; `HEAVYQ_STANCES=off` mutes
  one process (the hook and `ActiveFor` say nothing).
- **Time box:** a stance with `exit.timebox_minutes` is over when it runs out; the next command that touches the
  lane logs the exit as `timebox`.
- `stance status` shows the stance, why, minutes in it, the time left, the decision record and the evidence the
  triggers see right now, plus any untrusted repo stances.

## Injection

`heavyq hook claude-userpromptsubmit` (UserPromptSubmit) reads the prompt, matches the keywords of every
injectable stance and, only when the prompt asks for nothing and the lane is in no stance, reads the evidence.
On entry it prints `additionalContext` with the compact render once and records the state. It fails open (any
problem: no output, exit 0, a line in `hook.log`) under a 1 s budget and never changes or blocks a
prompt. Install it with `heavyq hook install claude --features stance` (the feature registers `UserPromptSubmit`;
combine it with the others, for example `--features queue,brief,stop,stance`).

**Re-injection after compaction:** the SessionStart hook (`brief` feature) appends the lane's active stance to the
brief, in its compact form, on startup, resume and after compaction, so compaction cannot erase it. In a
repository with no brief it injects the stance alone. The function behind it is
`stance.ActiveFor(lane string) (render string, ok bool)`: the same compact render, with the evidence the lane
entered on, for the lane's active stance, or `ok=false` when the lane is in none, is muted, is past its time box,
or the stance is gone or no longer trusted. It reads one lane file, the stance files and the trust list, changes
nothing and fails quiet. `Env.ActiveFor(cwd, lane)` is the same over explicit directories. `HEAVYQ_STANCES=off`
silences both hooks.

## Exit and the decision record

A stance with `exit.decision_record = true` (the built-in `breakthrough`) ends when the lane's decision record
passes a **structure check**, or when its time box runs out. `heavyq stance exit` runs the check, refuses with a
list of what is missing (exit code 1), and with `--force` leaves anyway; the exit is logged as `decision_record`,
`timebox`, `manual` (the stance has no condition) or `forced`. The check itself is advisory: it binds only where
a caller binds it (a Stop hook in steer mode `destroy`).

The record is `.heavyq/decisions/<item>.md` in the work tree. `<item>` is the item the lane holds a lease on, or
`--item`, or the lane's own name; an item id with `/` makes subdirectories. The directory is bookkeeping: it is
left out of the tree identity, so writing a record voids no proof. heavyq checks headings (any level, any case,
matched on a word of the title) and shape. It never grades the thinking: the human is the judge.

| Section (title contains) | Needs |
|---|---|
| `clean-slate` / `clean slate` | some text |
| `option` | a markdown table with at least 3 rows, one naming the **status quo** and a different one marked **radical** (the word, or a star) |
| `constraint` | some text using both the words REAL and INERTIA |
| `choice` / `chosen` | some text |
| `migration` | some text (one heading such as "Choice and migration" may carry both) |

```markdown
## Clean-slate answer
From scratch I would drop the refresh path and issue short-lived tokens.

## Options
| Option | Value | Cost |
|---|---|---|
| Status quo: keep retrying refresh | none | 0 |
| Incremental: back off the retries | fewer 401s | S |
| Radical: short-lived tokens, no refresh | no refresh bugs | M |

## Constraints
- Tokens must expire within an hour: REAL (security policy).
- Refresh lives in one module: INERTIA (it was written that way first).

## Choice and migration
Option 3, measured by the 401 rate. Behind a flag, then remove the refresh module.
```

## Measurement

`heavyq stance stats` rebuilds each stay of a lane in a stance from the events log and, per stance, shows how many
activations there were, how they started and ended, how many reached a proof, and the **median time and runs to
the first proof after entering** (the first passing run on a known tree, cache replays not counted) next to the
**lane's own baseline** (the median time and runs between its earlier proofs). It is descriptive: a lane that
enters a stance is usually a lane in trouble. A causal claim needs the randomised holdout arm from the mentor log,
which this command does not read; a keyword entry is explicit intent and is never held out.

## Trust

A repository's own stances (`.heavyq/stances`) are text that heavyq injects into an agent's context, so a repo you
just cloned must not be able to do that. They work like `direnv allow`:

- Until you run **`heavyq trust`** in the repository, its stances are **listed but never injected** (`stance ls`
  flags them, the hook and `ActiveFor` ignore them, `stance enter` refuses with the way out). Built-in and
  your own stances need no trust.
- `heavyq trust` stores a **content hash** of the stance files (names and bytes) for the repository's git common
  dir in `stance-trust.json`, beside the ledger (mode 0600). Any edit, new file, removed file or rename changes
  the hash and asks again. `heavyq trust --list` shows what is trusted, `--revoke` forgets the repository.
- `heavyq trust` is refused when it runs under a coding agent (`CLAUDECODE` and friends, see agent mode): a
  stance file could talk an agent into granting itself trust. Read the files, then run it from your terminal.
- An unreadable or damaged trust file trusts nothing.

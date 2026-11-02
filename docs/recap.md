# heavyq recap: how it is going and what landed

`heavyq recap` answers three questions with zero or one command: how is it going, what landed, what should I do
next. It reads the same facts as the [status line](statusline.md) (the flow state, the work items, the ledger) and
prints one screen.

```
heavyq recap [--since 2h] [--session S] [--run ID] [--json] [--verbose] [--ascii] [--no-notify]
```

```
◆ 5/9 proven  ✓ on track · ~16:00 (medium confidence, 9 proofs)  ▲ CI wait  ⚑ 2 needs you
next: heavyq item claim doc-7   (doc-7  needs: a Stripe test key)

merged into main (2)
  api-1  Login endpoint   git show --stat 3f2a9c1
  api-3  Rate limiting    git show --stat c0ffee1

proven, not on main (2)
  api-2  Token refresh    heavyq item done api-2
  ui-1   Form markup      heavyq item done ui-1

claimed, unproven (1)
  doc-3  ticked, no proof declared   heavyq item done doc-3 --run

blocked (2)
  doc-7  needs: a Stripe test key             heavyq item claim doc-7
  web  permission_prompt: Bash(npm run e2e)   heavyq top

wasted (1)
  go test ./internal/cli  failed 3x, 7m lost (api)   heavyq why api

in flight (1)
  web-1  web holds it until 14:30   heavyq item done web-1 --run
```

## The header and the next action

- **Header:** progress, the [forecast](statusline.md#the-forecast) with its ETA and confidence, the constraint
  (when significant) and how many things need you. With no flow state yet it says `forecast unknown`.
- **next:** the single action that would help most, chosen in the order a person would triage: a blocked item or
  a prompt that needs you; a forecast at risk (`heavyq top`); the same failure repeated; a ticked item nothing
  proves; a proven item whose box is not ticked; starting the next item when nothing is in flight. When none
  applies it says `nothing needs you`.

## The buckets

Empty buckets are left out; every line ends with the command that acts on it.

| Bucket | What is in it | Command |
|---|---|---|
| merged into main | proven in the window, and the proven commit is reachable from the default branch | `git show --stat <commit>`, or `heavyq item done <id>` if the box is not ticked |
| proven, not on main | proven in the window, the commit is not on the default branch | `heavyq item done <id>` (ticks the box), then merge |
| claimed, unproven | ticked (or proven on older code, `stale`) with nothing proving it now | `heavyq item done <id> --run` |
| blocked | an item blocked on a need, and what the flow state says waits for you (a permission prompt has no item) | `heavyq item claim <id>` once answered; `heavyq top` for a prompt |
| wasted | the same failing run repeated, with the count and the time it cost | `heavyq why <lane>` |
| in flight | items a lane holds, and until when the lease runs | `heavyq item done <id> --run` |

- **Merged** means reachable from the default branch (`origin/HEAD`, else `origin/main`, `origin/master`, `main`,
  `master`). A squash or rebase merge leaves the original commit unreachable, so it reads `proven, not on main`:
  the recap never claims more than git can show. Without a default branch the buckets merge into one called
  `proven`. heavyq does not call `gh`: no network, nothing to authenticate.
- **Wasted** groups failing runs in the window by the ledger's `run_id` (the tree, exact argv, subdir, environment
  and base: the one definition of "the same run"), else `cmd_hash`. Two or more is waste. A failure that started over a
  resource limit (`under_load`) is the machine's fault and a cache replay is not a run, so neither counts.
- Item text is data anyone could have written: control characters are removed and lines are cut to a width.

## Flags

- `--since DUR` is how far back (`90m`, `2h`; the default is the declared run's start, else 4 h). The ledger is
  read through its index and tail window (the last 7 days at most).
- `--session S` keeps this session's items and runs. `--run ID` looks back to the start of the run declared in the
  flow state (`run` is set by `heavyq preflight`); an unknown id is an error.
- `--verbose` adds the lanes, with how long each has been in its state, and the time of each line.
- `--ascii` replaces the glyphs (also automatic in a locale that is not UTF-8). `--no-notify` skips the check below.
- `--json` is the same document for agents. Schema 1: `schema`, `generated`, `since`, `goal`, `merge_ref`, `progress`
  (`proven`, `total`), `forecast` (`status`, `confidence`, `done`, `eta`, `reason`, `samples`), `constraint`,
  `needs_you`, `best` (`command`, `why`), the six buckets `merged`, `proven_not_merged`, `claimed_unproven`,
  `blocked`, `wasted`, `in_flight` (each line has `text`, `action`, and when known `id`, `lane`, `at`, `count`,
  `commit`, `lost_s`) and `lanes` with `--verbose`. It is built from the same value the text is, so they cannot differ.

## Notifications

A recap also runs the [notification check](statusline.md#notifications): if something needs you, the forecast
turned at risk or everything is proven, and that has not been pushed yet, one local notice goes out. Never twice
for the same state; at most 5 a day.

## Developing

`recap.Build` and `recap.Render` are pure; the goldens (text, ASCII, verbose and JSON) are in
`internal/recap/testdata`. After an intended change: `go test ./internal/recap -update`, then read the diff.

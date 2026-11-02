# heavyq top: the queue in the pane next to your agents

`heavyq top` is a terminal monitor. It reads the same snapshot as `heavyq status --json` and the
[dashboard](dashboard.md) (no new logic, no daemon) and redraws it every 2 s, so it fits a tmux or kitty
pane beside the agents, over ssh included. The dashboard stays for rich views and sharing.

```bash
heavyq top                      # read-only; the last display mode you used (full the first time)
heavyq top --control            # the same requests as `heavyq dash --control`, from the keyboard
heavyq top --mode focus         # a tiny pane: only what needs you
heavyq top --line               # one line on stdout, once: tmux status-right
```

## What it shows

```
heavyq  2 running, 2 waiting, 4 proven, 1 unproven              12:04:31  2s ago
NEEDS YOU (3) ------------------------------------------------------------------
  ● red gate  api  tree 3f2a9c1  go test ./...  failed 2x
  ▲ starved  agent-ad17ea  go test ./internal/cli
  ● blocked  docs  item doc-7  needs a Stripe test key   heavyq item claim doc-7
LANES --------------------------------------------------------------------------
  ● run   api           p4  linuxi/h1  1m10s   go test -race ./...
  ● run   docs          p5  local/h1   2m31s   npm run build
  ○ wait  agent-ad17ea  p4  #1 auto    14m     load 16.00 >= 9.0  go test ./int…
  ○ wait  web           p4  #2 auto    40s     load 16.00 >= 9.0  npm run e2e
MACHINES -----------------------------------------------------------------------
  local   ▪▪|▫▫▫▫▫  9.6G free  load 12.3/16.0  slots free h 1/2 l 1/1
  linuxi  ▪▪▫▫|▫▫▫  5.4G free  load 16.0/9.0  ! load 16.00 >= 9.0
  mac     ▪▪▪▪|▫▫▫  8.3G free  load 1.5/12.0  slots free h 2/2 l 1/1
POOLS --------------------------------------------------------------------------
  pg 2/3  port 1/100
  2 agents wait because linuxi is over its load limit: agent-ad17ea is first and
  the bypass applies in 6m.
  Outside the queue, 33 chrome processes hold 7.5 GB, everything else 11 GB.
⏎ why  / filter  m mode full  ? help  q quit  read-only: --control to act
```

- **Header:** the queue in a phrase, the clock, and how old the data is. After three missed intervals it
  says `STALE 9s`; a failed build says `SNAPSHOT FAILED` (the last good snapshot stays on screen).
- **NEEDS YOU:** the Tower's exceptions (`internal/snapshot`), most urgent first: a red gate bound to its tree,
  a loop, a starved head of queue, a machine that is down or paused, a blocked item. With none, one calm line.
- **LANES:** one row per ticket: state, lane, priority, where (`machine/slot`, or `#position target`), time,
  then for a waiting ticket the short reason it cannot start and the anti-starvation countdown, then the
  command; with room, the lane's gate and work item.
- **MACHINES:** a RAM bar whose `|` marks the free RAM one heavy job needs (a fill that stops short of the
  marker means nothing fits), load against its limit, the slots, what blocks admission as a sentence
  (`! load 16.00 >= 9.0`), and for the local machine what runs outside the queue and the biggest outsider.
  Pools and the story sentences follow.
- At **140 columns or more** a right column carries machines, pools and the story; this is the same screen,
  not another mode. Below **60 columns** rows collapse (state, lane, time, reason); below 24 it only says
  the window is too narrow.

## Display modes

`--mode NAME` picks one, `m` cycles through them, and the last one is remembered in
`$XDG_STATE_HOME/heavyq/top-mode` (default `~/.local/state/heavyq/top-mode`). An explicit `--mode` wins.

| Mode | Shows | For |
|---|---|---|
| `full` | the screen above | a pane of 60 to 140 columns, or more with the right column |
| `focus` | the summary line and NEEDS YOU, nothing else | a tiny pane that is quiet until something needs you |
| `lanes` | the lane table with every column: where, wait, run, gate tree, item, command | watching a busy queue |
| `machines` | machines with their slots, pools with holders, and what runs outside the queue | "why is this box slow" |
| `line` | one line | a one-row pane; `--line` prints it once for tmux |

`focus` at 80 columns:

```
heavyq  2 running, 2 waiting, 4 proven, 1 unproven              12:04:31  2s ago
NEEDS YOU (3) ------------------------------------------------------------------
  ● red gate  api  tree 3f2a9c1  go test ./...  failed 2x
  ▲ starved  agent-ad17ea  go test ./internal/cli
  ● blocked  docs  item doc-7  needs a Stripe test key   heavyq item claim doc-7
⏎ why  / filter  m mode focus  ? help  q quit  read-only: --control to act
```

`lanes` at 80 columns:

```
heavyq  2 running, 2 waiting, 4 proven, 1 unproven              12:04:31  2s ago
LANES --------------------------------------------------------------------------
  state   lane          wait    run     gate
  ● run   api           2s      1m10s   FAILED 3f2a9c1  go test -race ./...
  ● run   docs          0s      2m31s   proven          npm run build
  ○ wait  agent-ad17ea  14m     -       unproven        load 16.00 >= 9.0
  ○ wait  web           40s     -       proven          load 16.00 >= 9.0
⏎ why  / filter  m mode lanes  ? help  q quit  read-only: --control to act
```

`machines` at 80 columns:

```
heavyq  2 running, 2 waiting, 4 proven, 1 unproven              12:04:31  2s ago
MACHINES -----------------------------------------------------------------------
  local   ▪▪|▫▫▫▫▫  9.6G free  load 12.3/16.0  slots free h 1/2 l 1/1
          h1 docs  h2 free  l1 free
  linuxi  ▪▪▫▫|▫▫▫  5.4G free  load 16.0/9.0  ! load 16.00 >= 9.0
          h1 api  h2 free  l1 free
  mac     ▪▪▪▪|▫▫▫  8.3G free  load 1.5/12.0  slots free h 2/2 l 1/1
          h1 free  h2 free  l1 free
POOLS --------------------------------------------------------------------------
  pg 2/3  pg-1 web, pg-2 api
  port 1/100  4101 docs
OUTSIDE THE QUEUE --------------------------------------------------------------
  chrome          33 procs     35% cpu    7.5G
  code            14 procs     12% cpu    2.8G
  slack            6 procs      2% cpu    980M
  total           71 procs     61% cpu   18.0G
⏎ why  / filter  m mode machines  ? help  q quit  read-only: --control to act
```

`line` (80 columns; `--line` uses 200, the status bar cuts it):

```
hq 2 run 2 wait 3 need you | local 9.6G load 12.3/16.0 | linuxi 5.4G load 16.0/…
```

tmux: `set -g status-right '#(heavyq top --line)'` with `set -g status-interval 5`. `--line` probes the
remotes like `heavyq status` does, so keep the interval at a few seconds. `--once [--mode M] [--width N]`
prints one frame of any mode, without colour when stdout is not a terminal.

## Keys

| Key | Does |
|---|---|
| `j` `k`, arrows | move the selection; `g` `G` first and last row; `Tab` next section |
| `Enter` | **why**: the same explanation as `heavyq why <lane>`, or the evidence of an exception |
| `/` | filter lanes by lane, machine or command (`Enter` keeps it, `Esc` clears it) |
| `m` | next display mode |
| `r` | refresh now |
| `?` | help; `q` or `Ctrl-C` quit; `Ctrl-Z` suspends and restores the terminal |
| `y` | copy the selected command to the clipboard (OSC 52; tmux needs `set -g set-clipboard on`) |
| `c` | cancel the selected job, after an inline `y/n` (**--control**) |
| `+` `-` | raise or lower a waiting job's priority, 1 to 9 (**--control**) |
| `p` | pause or resume the selected machine, or the one the selected job runs on (**--control**) |

Without `--control` the footer says `read-only` and `c + - p` only explain how to enable them. With it, the
actions are requests in the control mailbox (`<queue dir>/ctl`) that the job's own heavyq applies on its next
poll, with the checks of the dashboard's POST endpoints: nothing signals a process from `top`.
`s` (steer) is not there until stance injection ships. `--bell` rings the terminal bell when a new
exception appears.

## Terminal behaviour

- Alternate screen, raw mode, hidden cursor, no mouse. The terminal is reset at start (mouse reporting and
  bracketed paste off) and restored on exit, on SIGINT, SIGTERM and SIGHUP, around `Ctrl-Z` and
  SIGTSTP/SIGCONT (it repaints on continue), and when a panic unwinds.
- Frames are written as one buffer, wrapped in synchronized-output markers (DEC 2026; terminals that do not
  know it ignore them), and only the lines that changed are sent. Every line ends with an SGR reset.
- Resizes are debounced (75 ms) and a 0x0 or absurd size is ignored.
- Snapshots are built on one goroutine, never two at once (a slow remote probe delays the next build, it does
  not pile up); the screen redraws at about 10 fps so the counters tick between snapshots.
- Colour has three roles (red needs you, yellow look at it, green fine), each paired with a glyph or a word, on
  the 16-colour palette so your theme decides the shades. `NO_COLOR` (any value) drops the colour and keeps
  bold and reverse. `TERM=dumb` sends no escape sequences. A locale that is not UTF-8, or `--ascii`, replaces
  the glyphs with `* o ! # . |` and any other non-ASCII text with `?`.
- Status glyphs (`●` `○` `▲`) are always followed by a space, and the things that must line up (the RAM bar,
  rules) use characters that are one cell wide everywhere, so a terminal that draws ambiguous-width characters
  double cannot make text touch or shift them.
- Widths are measured in cells (`internal/tui/width`: combining marks and format characters 0, East Asian wide
  and fullwidth 2, else 1), never in bytes, and text is never cut inside a character.

## Not built

A wide split board (that is `heavyq dash`), resource history charts, a process tree, themes, a command
palette, mouse support, spinners, emoji. Command lines go through `internal/redact` before they reach the
snapshot, as everywhere else.

## Developing

The renderer is pure: `render.Frame(snapshot, mode, view, options)` returns the lines, so the golden frames in
`internal/tui/render/testdata/golden` (80, 120, 160 and 200 columns for `full`, 80 and 160 for each other mode,
plus `NO_COLOR`, ASCII, `TERM=dumb`, narrow, selection, scrolling, filter, prompt, help and why variants)
are plain text, compared after stripping SGR; every line must be no wider than the terminal. After an
intended change: `go test ./internal/tui/render -update`, then read the diff. The terminal restore is tested on
a real pty (Linux) in `internal/tui/term`. `golang.org/x/term` (raw mode and size) is the one added dependency.

<!-- ci check -->

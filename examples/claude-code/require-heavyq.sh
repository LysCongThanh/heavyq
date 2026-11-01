#!/usr/bin/env bash
# Claude Code PreToolUse hook (matcher: "Bash"): blocks heavy commands that are
# not wrapped in heavyq and tells the agent how to rerun them. Requires jq.
#
# Since v0.2 heavyq has a native hook that rewrites the command instead of
# blocking it: `heavyq hook install claude` (see README). Keep this script if
# you prefer the agent to see and type the heavyq command itself.
#
# Install:
#   cp require-heavyq.sh ~/.claude/hooks/ && chmod +x ~/.claude/hooks/require-heavyq.sh
#   and add to ~/.claude/settings.json:
#   {"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"~/.claude/hooks/require-heavyq.sh"}]}]}}
#
# Exit 2 = block the tool call; stderr is shown to the agent.
# Tune HEAVY_PATTERN to your stack (extended regex, matched against the command).
set -uo pipefail

HEAVY_PATTERN="${HEAVYQ_HOOK_PATTERN:-(^|[;&|(]|[[:space:]])((npm|pnpm|yarn|bun)( run)? (test|build|e2e|test:[a-z]+)|npx (vitest|jest|playwright)|go (test|build)|cargo (test|build|nextest)|pytest|tox|gradle|mvn|docker (build|compose up)|make( |$))}"

cmd="$(jq -r '.tool_input.command // empty' 2> /dev/null)" || exit 0
[[ -n "$cmd" ]] || exit 0
[[ "$cmd" == *heavyq* ]] && exit 0
[[ "$cmd" =~ $HEAVY_PATTERN ]] || exit 0

cat >&2 << EOF
This is a heavy command and other agents share this machine. Run it through the queue:
  heavyq 4 --lane \${HEAVYQ_LANE:-<your-worktree>} --max-wait 9m -- bash -c '<the same command>'
Use priority 2 if it blocks a merge, 6 for background checks, add --light for lint/typecheck.
Exit 75 means the queue is busy: wait and retry.
EOF
exit 2

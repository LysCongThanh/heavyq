#!/usr/bin/env bash
# Prepares the environment recorded in coalesce-demo.tape (sourced, hidden).
# A clean git repo and a stub `npm test` that counts its real executions.
D="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export PATH="$D/coalesce-bin:$PATH"
W="$(mktemp -d)"
export HEAVYQ_DIR="$W/queue" HEAVYQ_HISTORY="$W/history.jsonl" HEAVYQ_CONFIG="$W/config.toml" \
  XDG_CACHE_HOME="$W/cache" DEMO_RUNS="$W/runs"
printf '[queue]\npoll = "300ms"\nresource_gate = false\n' > "$HEAVYQ_CONFIG"
mkdir -p "$W/shop" && cd "$W/shop" && git init -q -b main && echo '{}' > package.json && echo '*.log' > .gitignore \
  && git add . && git -c user.name=demo -c user.email=demo@example.com commit -qm init
set +m
PS1='$ '
clear

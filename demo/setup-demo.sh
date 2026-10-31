#!/usr/bin/env bash
# Prepares the environment recorded in demo.tape (sourced, hidden in the GIF).
# Stub npm/pnpm/cargo burn CPU so "load by lane" has something to show; the
# "buildbox" remote is this machine reached through `sh -c`.
D="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export PATH="$D/bin:$PATH"
W="$(mktemp -d)"
export HEAVYQ_DIR="$W/queue" HEAVYQ_HISTORY="$W/history.jsonl" HEAVYQ_CONFIG="$W/config.toml"
cat > "$HEAVYQ_CONFIG" <<TOML
[queue]
poll = "500ms"
[local]
heavy_slots = 1
light_slots = 1
max_load_ratio = 8.0   # the recording box is busy with other work
[[remote]]
name = "buildbox"
transport = ["sh", "-c"]
work_dir = "$W/buildbox"
git_url = "$W/buildbox/git"
heavy_slots = 1
max_load_ratio = 8.0
TOML
mkdir -p "$W/shop" && cd "$W/shop" && git init -q -b main && echo '{}' > package.json \
  && git add . && git -c user.name=demo -c user.email=demo@example.com commit -qm init
heavyq 3 --lane api -- npm test > /dev/null 2>&1 &
sleep 1
heavyq 4 --lane web -- pnpm build > /dev/null 2>&1 &
sleep 1.5
heavyq 6 --light --lane docs -- npm run lint > /dev/null 2>&1 &
heavyq 5 --lane worker -- cargo test > /dev/null 2>&1 &
sleep 0.3
heavyq 2 --lane checkout -- npm test > /dev/null 2>&1 &
sleep 1.5
PS1='$ '
clear

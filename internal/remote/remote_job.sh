#!/usr/bin/env bash
# heavyq remote job. Runs ON the remote machine, started by heavyq through the
# configured transport; not meant to be run by hand.
#   args: <work_dir> <repo> <sha> <label> <slot>
#   env:  HEAVYQ_CMD_B64     command line (shell syntax, base64)
#         HEAVYQ_SUBDIR      subdirectory of the repo to run in ("" = root)
#         HEAVYQ_WITH        sibling repos "name@sha,name@sha"
#         HEAVYQ_BASE_REF / HEAVYQ_BASE_SHA   recreate refs/remotes/<ref>
#         HEAVYQ_HOOK_B64    optional dependency-cache hook (shell, base64)
#         HEAVYQ_WATCH_STDIN=1  stdin is a lifeline: EOF means heavyq died
# Exit: the command's code (97/98 from the command become 96);
#       97 = slot busy, 98 = setup failed (nothing was run).
set -uo pipefail
W="$1"; REPO="$2"; SHA="$3"; LABEL="$4"; SLOT="$5"
case "$W" in /*) ;; *) W="$HOME/$W" ;; esac
safe() { [[ "$1" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; }
fail() { echo "heavyq-job: $*" >&2; exit 98; }
safe "$REPO" && [[ "$SHA" =~ ^[0-9a-f]{40}$ ]] && safe "$LABEL" && safe "$SLOT" || fail "bad arguments"
mkdir -p "$W/git" "$W/wt" "$W/slots" "$W/cache" || fail "cannot create $W"
exec 9> "$W/slots/$SLOT.lock" || fail "cannot open slot lock"
flock -w 5 9 || { echo "heavyq-job: slot $SLOT is busy (used outside the queue?)" >&2; exit 97; }

ID="${SHA:0:8}-$$"
BARE="$W/git/$REPO.git"
JOB="$W/wt/$REPO-$ID.d"
WT="$JOB/$REPO"
SIB_NAMES=(); SIB_SHAS=()
if [[ -n "${HEAVYQ_WITH:-}" ]]; then
  IFS=, read -r -a _sibs <<< "$HEAVYQ_WITH"
  for _s in "${_sibs[@]}"; do
    _n="${_s%%@*}"; _h="${_s#*@}"
    safe "$_n" && [[ "$_n" != "$REPO" && "$_h" =~ ^[0-9a-f]{40}$ ]] || fail "bad HEAVYQ_WITH entry '$_s'"
    SIB_NAMES+=("$_n"); SIB_SHAS+=("$_h")
  done
fi
BASE_REF="${HEAVYQ_BASE_REF:-}"; BASE_SHA="${HEAVYQ_BASE_SHA:-}"
if [[ -n "$BASE_REF" ]]; then
  [[ "$BASE_REF" =~ ^[A-Za-z0-9._-]+/[A-Za-z0-9._/-]+$ && "$BASE_REF" != *..* && "$BASE_SHA" =~ ^[0-9a-f]{40}$ ]] || fail "bad base ref"
fi

kill_group() { # TERM the command's process group, then KILL after 2 s
  local g="$1" i
  kill -0 -- "-$g" 2> /dev/null || return 0
  kill -TERM -- "-$g" 2> /dev/null
  for i in 1 2 3 4 5 6 7 8 9 10; do kill -0 -- "-$g" 2> /dev/null || return 0; sleep 0.2; done
  kill -KILL -- "-$g" 2> /dev/null || true
}
CHILD=""; WATCH=""
cleanup() {
  trap '' TERM HUP INT
  if [[ -n "$WATCH" ]]; then pkill -P "$WATCH" 2> /dev/null; kill "$WATCH" 2> /dev/null; fi
  [[ -z "$CHILD" ]] || kill_group "$CHILD"
  git -C "$BARE" worktree remove --force "$WT" > /dev/null 2>&1
  local i
  for i in "${!SIB_NAMES[@]}"; do
    git -C "$W/git/${SIB_NAMES[$i]}.git" worktree remove --force "$JOB/${SIB_NAMES[$i]}" > /dev/null 2>&1
    git -C "$W/git/${SIB_NAMES[$i]}.git" worktree prune > /dev/null 2>&1
  done
  [[ "$JOB" == "$W"/wt/"$REPO"-*.d && -d "$JOB" ]] && rm -rf -- "$JOB"
  git -C "$BARE" worktree prune > /dev/null 2>&1
  # the base ref is shared by jobs of the same repo: drop it only when no other worktree uses it
  if [[ -n "$BASE_REF" && "$(git -C "$BARE" worktree list 2> /dev/null | wc -l)" -le 1 ]]; then
    git -C "$BARE" update-ref -d "refs/remotes/$BASE_REF" "$BASE_SHA" > /dev/null 2>&1
  fi
  return 0
}
trap cleanup EXIT; trap 'exit 143' TERM; trap 'exit 129' HUP; trap 'exit 130' INT
if [[ "${HEAVYQ_WATCH_STDIN:-}" == 1 ]]; then
  # A background job's stdin defaults to /dev/null, so hand the lifeline over on fd 8.
  exec 8<&0
  ( cat <&8 > /dev/null 2>&1; kill -TERM $$ 2> /dev/null ) > /dev/null 2>&1 9>&- &
  WATCH=$!
  exec 8<&-
fi
exec < /dev/null

[[ -d "$BARE" ]] || fail "no bare repo $BARE (push first)"
git -C "$BARE" cat-file -e "$SHA^{commit}" 2> /dev/null || fail "commit $SHA not in $BARE"
mkdir -p "$JOB" || fail "cannot create $JOB"
git -C "$BARE" worktree add --detach -q "$WT" "$SHA" 9>&- || fail "cannot create worktree"
if [[ -n "$BASE_REF" ]]; then
  git -C "$BARE" update-ref "refs/remotes/$BASE_REF" "$BASE_SHA" || fail "cannot create base ref $BASE_REF"
fi
for i in "${!SIB_NAMES[@]}"; do
  SB="$W/git/${SIB_NAMES[$i]}.git"
  git -C "$SB" cat-file -e "${SIB_SHAS[$i]}^{commit}" 2> /dev/null || fail "commit ${SIB_SHAS[$i]} not in $SB"
  git -C "$SB" worktree add --detach -q "$JOB/${SIB_NAMES[$i]}" "${SIB_SHAS[$i]}" 9>&- || fail "cannot create sibling worktree ${SIB_NAMES[$i]}"
done
SUB="${HEAVYQ_SUBDIR:-}"
if [[ -n "$SUB" ]]; then
  [[ "$SUB" =~ ^[A-Za-z0-9._/-]+$ && "$SUB" != *..* && -d "$WT/$SUB" ]] || fail "bad subdirectory '$SUB'"
fi
cd "$WT/$SUB" || fail "cannot enter worktree"
echo "heavyq-job: $(hostname 2> /dev/null || echo remote): $REPO@${SHA:0:12}, lane $LABEL, slot $SLOT" >&2
export HEAVYQ_JOB_ID="$ID" HEAVYQ_WORKTREE="$WT" HEAVYQ_CACHE_DIR="$W/cache"
if [[ -n "${HEAVYQ_HOOK_B64:-}" ]]; then
  HOOK="$(printf '%s' "$HEAVYQ_HOOK_B64" | base64 -d)" || fail "cannot decode cache hook"
  bash -c "$HOOK" 9>&- || fail "cache hook failed"
fi
CMD="$(printf '%s' "${HEAVYQ_CMD_B64:-}" | base64 -d)" || fail "cannot decode command"
[[ -n "$CMD" ]] || fail "empty command"
# 9>&-: the command never inherits the slot lock, so daemons it leaves behind cannot pin the slot.
setsid bash -c "$CMD" 9>&- &
CHILD=$!
wait "$CHILD"; rc=$?
kill_group "$CHILD"; CHILD=""
[[ "$rc" -ne 97 && "$rc" -ne 98 ]] || rc=96
exit "$rc"

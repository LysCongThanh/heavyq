#!/usr/bin/env bash
# Prepares the environment recorded in proof.tape (sourced, hidden in the GIF).
# Builds heavyq from this checkout, creates a tiny Go module in a fresh temp git repo whose heavyq.toml gate
# `ready` runs a real `go test`, and proves the committed code once. Nothing here fakes heavyq's output:
# the GIF shows what the binary prints. Needs go and git.  Usage: source docs/tapes/proof-setup.sh
HQ_SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
W="$(mktemp -d)"
mkdir -p "$W/bin"
(cd "$HQ_SRC" && go build -o "$W/bin/heavyq" .) || { echo "proof-setup: go build failed" >&2; return 1; }
export PATH="$W/bin:$PATH"
# Isolated queue and ledger; human output even when recorded from inside an agent session.
export HEAVYQ_DIR="$W/queue" HEAVYQ_HISTORY="$W/history.jsonl" HEAVYQ_OUTPUT=human
export HEAVYQ_CONFIG="$W/config.toml"
printf '[local]\nmax_load_ratio = 8.0   # the recording box may be busy\n' > "$HEAVYQ_CONFIG"

mkdir -p "$W/shop" && cd "$W/shop" || return 1
git init -q -b main
cat > go.mod <<'GOMOD'
module shop

go 1.22
GOMOD
cat > cart.go <<'GO'
package shop

// Total returns the price of n items, in cents.
func Total(n, cents int) int { return n * cents }
GO
cat > cart_test.go <<'GO'
package shop

import "testing"

func TestTotal(t *testing.T) {
	if got := Total(3, 250); got != 750 {
		t.Fatalf("Total(3, 250) = %d, want 750", got)
	}
}
GO
cat > heavyq.toml <<'TOML'
[[gate]]
name = "ready"
run  = ["go test -count=1 ./..."]
TOML
git add . && git -c user.name=demo -c user.email=demo@example.com commit -qm "cart: total"
# Warm the Go build cache and record a proof of the committed code, so the recording is quick.
heavyq gate run > /dev/null 2>&1 || { echo "proof-setup: the demo gate failed" >&2; return 1; }
export PS1='\[\e[32m\]shop\[\e[0m\] $ '
clear

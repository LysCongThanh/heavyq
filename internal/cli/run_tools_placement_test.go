package cli

import (
	"reflect"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/proof"
)

// A gate command runs inside the wrapper of proof.Argv: placement reads the command, not the wrapper.
func TestArgvToolsSeesThroughTheGateWrapper(t *testing.T) {
	for _, c := range []struct {
		argv []string
		want []string
	}{
		{proof.Argv("env HEAVYQ_TEST_NORACE=1 go test -skip 'x' ./..."), []string{"sh", "env", "go"}},
		{proof.Argv("gofmt -l . | (! grep .)"), []string{"sh", "gofmt"}},
		{proof.Argv("bash scripts/check.sh"), []string{"sh", "bash"}},
		{[]string{"npx", "tsc"}, []string{"npx"}},
		{append(proof.Argv("go test"), "extra"), []string{"sh", "command"}}, // not the wrapper's shape: read as any sh -c
	} {
		if got := argvTools(c.argv); !reflect.DeepEqual(got, c.want) {
			t.Errorf("argvTools(%q) = %q, want %q", c.argv, got, c.want)
		}
	}
}

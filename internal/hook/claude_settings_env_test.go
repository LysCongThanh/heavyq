package hook

import (
	"strings"
	"testing"
)

func TestMergeEnvSetsKeepsAndIsIdempotent(t *testing.T) {
	b, changed, err := MergeEnv(nil, "CLAUDE_CODE_STOP_HOOK_BLOCK_CAP", "0")
	if err != nil || !changed || !strings.Contains(string(b), `"CLAUDE_CODE_STOP_HOOK_BLOCK_CAP": "0"`) {
		t.Fatalf("%v %v\n%s", changed, err, b)
	}
	b2, changed, err := MergeEnv(b, "CLAUDE_CODE_STOP_HOOK_BLOCK_CAP", "0")
	if err != nil || changed || string(b2) != string(b) {
		t.Fatalf("setting it again must change nothing: %v %v", changed, err)
	}
	orig := []byte(`{"model":"opus","env":{"FOO":"1","CLAUDE_CODE_STOP_HOOK_BLOCK_CAP":"3"},"hooks":{"Stop":[]}}`)
	b3, changed, err := MergeEnv(orig, "CLAUDE_CODE_STOP_HOOK_BLOCK_CAP", "0")
	if err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	for _, keep := range []string{`"FOO": "1"`, `"model": "opus"`, `"Stop"`, `"CLAUDE_CODE_STOP_HOOK_BLOCK_CAP": "0"`} {
		if !strings.Contains(string(b3), keep) {
			t.Errorf("lost %q:\n%s", keep, b3)
		}
	}
}

func TestMergeEnvRefusesWrongShapes(t *testing.T) {
	for _, bad := range []string{`[]`, `{"env":[]}`, `{"env":"x"}`, `{not json`} {
		if _, _, err := MergeEnv([]byte(bad), "K", "0"); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

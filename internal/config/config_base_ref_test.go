package config

import "testing"

// queue.base_ref is "", a remote/branch ref, or exactly "none" (no base ref, which also switches off the
// default of origin's default branch).
func TestQueueBaseRefAcceptsNoneAndRefsOnly(t *testing.T) {
	for _, in := range []string{"", BaseRefNone, "origin/main", "up.stream/release/1.x"} {
		c, err := Load(writeConfig(t, "[queue]\nbase_ref = \""+in+"\"\n"), true)
		if err != nil || c.Queue.BaseRef != in {
			t.Errorf("base_ref %q: got %q, %v", in, c.Queue.BaseRef, err)
		}
	}
	for _, bad := range []string{"None", "NONE", "no", "main", "origin/../x", "origin/main one", "origin/$(x)"} {
		if _, err := Load(writeConfig(t, "[queue]\nbase_ref = \""+bad+"\"\n"), true); err == nil {
			t.Errorf("accepted base_ref %q", bad)
		}
	}
}

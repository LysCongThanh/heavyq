package config

import (
	"strings"
	"testing"
	"time"
)

// TOML 1.0 has nan, and every comparison with NaN is false, so range checks
// let it through: the load gate turned silently off and status --json failed.
func TestRefusesNaN(t *testing.T) {
	for _, tc := range []struct{ body, key string }{
		{"[local]\nmax_load_ratio = nan\n", "max_load_ratio"},
		{"[[remote]]\nname = \"b\"\nssh = \"b\"\nmax_load_ratio = nan\n", "max_load_ratio"},
		{"[mentor]\nmin_lift_pp = nan\n", "min_lift_pp"},
	} {
		if _, err := Load(writeConfig(t, tc.body), true); err == nil || !strings.Contains(err.Error(), tc.key) {
			t.Errorf("%q: err = %v", tc.body, err)
		}
	}
}

// A negative value is a typo, not "use the default": refuse it and name the
// key. Zero (or absent) still means the default.
func TestRefusesNegativeDurationsAndWeights(t *testing.T) {
	for key, body := range map[string]string{
		"queue.poll":            "[queue]\npoll = \"-2s\"\n",
		"queue.force_after":     "[queue]\nforce_after = \"-5m\"\n",
		"queue.remote_down_max": "[queue]\nremote_down_max = \"-1m\"\n",
		"queue.hold_for":        "[queue]\nhold_for = \"-1m\"\n",
		"queue.probe_timeout":   "[queue]\nprobe_timeout = \"-1s\"\n",
		"queue.load_weight_mb":  "[queue]\nload_weight_mb = -3\n",
		"hook.max_wait":         "[hook]\nmax_wait = \"-5m\"\n",
		"cache.ttl":             "[cache]\nttl = \"-1h\"\n",
	} {
		if _, err := Load(writeConfig(t, body), true); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("%s: err = %v", key, err)
		}
	}
	c, err := Load(writeConfig(t, "[queue]\nforce_after = \"0s\"\nload_weight_mb = 0\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	if c.Queue.ForceAfter.Duration != defaultForceAfter || c.Queue.LoadWeightMB != defaultLoadWeightMB {
		t.Fatalf("zero must mean the default: %+v", c.Queue)
	}
}

// Every waiting ticket runs a scheduling round (queue flock, prune, list,
// local metrics) per poll; "1ms" would turn each waiter into a busy loop on
// the machine heavyq protects.
func TestQueuePollHasAFloor(t *testing.T) {
	for _, v := range []string{"1ns", "1ms", "49ms"} {
		_, err := Load(writeConfig(t, "[queue]\npoll = \""+v+"\"\n"), true)
		if err == nil || !strings.Contains(err.Error(), "queue.poll") {
			t.Errorf("poll %s: err = %v", v, err)
		}
	}
	c, err := Load(writeConfig(t, "[queue]\npoll = \"50ms\"\n"), true)
	if err != nil || c.Queue.Poll.Duration != 50*time.Millisecond {
		t.Fatalf("50ms poll: %v %+v", err, c)
	}
}

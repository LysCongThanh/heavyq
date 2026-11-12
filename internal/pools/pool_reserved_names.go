package pools

// reservedNames are the pool names whose variable, HEAVYQ_<NAME>, heavyq
// itself sets or reads. A run exports its pool values after the environment
// it inherited, so a pool of such a name would hand its value to the command
// and to every heavyq the command runs ("history" would move their ledger,
// "flow" = off would stop flow refresh). They are refused when a pool is
// defined and when it is asked for.
//
// TestReservedCoversEveryVariableInTheRepository fails when a HEAVYQ_ variable
// is written anywhere in the repository and is missing here.
var reservedNames = []string{
	// Set for every local command.
	"machine", "slot", "lane", "active",
	// Defaults and switches read from the environment (man page, docs/configuration.md).
	"config", "dir", "remote", "auto", "session", "item", "output", "agent", "history",
	"steer", "mentor", "mentor_seed", "notify", "notify_budget", "stances",
	"flow", "flow_gh", "flow_quota_grace",
	// The remote job protocol (internal/remote) and what a cache hook is given.
	"cmd_b64", "subdir", "with", "watch_stdin", "base_ref", "base_sha", "env_path", "hook_b64",
	"job_id", "worktree", "cache_dir", "cmd_len",
	// Test switches, the installer and the example hook script.
	"test", "test_fake_metrics", "test_norace", "test_network", "test_mac_remote", "test_mac_env_path",
	"update_golden", "lease_test_helper", "lease_test_helper_exit", "release_base", "hook_pattern",
	"slots_pidns_helper",
}

// reserved is reservedNames as a set.
var reserved = func() map[string]bool {
	m := make(map[string]bool, len(reservedNames))
	for _, n := range reservedNames {
		m[n] = true
	}
	return m
}()

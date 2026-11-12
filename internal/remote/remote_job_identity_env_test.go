package remote

import (
	"strings"
	"testing"
)

// The run's identity variables (cache.env) reach the command exactly as heavyq recorded them: set ones with
// their values, unset ones removed, whatever the remote's own environment holds.
func TestRemoteJobGivesTheCommandItsIdentityVariables(t *testing.T) {
	m, sha := shippedApp(t)
	t.Setenv("HQ_IDENTITY_GONE", "remote-side value") // the local transport passes the caller's environment on
	rc, out, errs := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1",
		Argv:  []string{"sh", "-c", `printf '%s|%s|%s' "$HQ_IDENTITY_SET" "${HQ_IDENTITY_GONE-unset}" "$HQ_IDENTITY_QUOTED"`},
		Env:   []string{"HQ_IDENTITY_SET=one", "HQ_IDENTITY_QUOTED=it's \"$HOME\" $(true)"},
		Unset: []string{"HQ_IDENTITY_GONE"}})
	if want := `one|unset|it's "$HOME" $(true)`; rc != 0 || out != want {
		t.Fatalf("rc=%d out=%q, want %q\n%s", rc, out, want, errs)
	}
}

// The identity variables used to travel in an environment variable of the job, and everything in the job's
// environment goes into the transport's one exec argument (at most 128 KiB, with the job script). Values that
// each fit a local environment but together do not fit that argument made the run fail to start (exit 77).
// They travel on stdin with the command line now, as a long argv does.
func TestRemoteJobGivesTheCommandIdentityVariablesLargerThanOneExecArgument(t *testing.T) {
	m, sha := shippedApp(t)
	big := strings.Repeat("v", 60*1024)
	env := []string{"HQ_BIG_A=" + big, "HQ_BIG_B=" + big, "HQ_BIG_C=" + big}
	rc, out, errs := runJob(t, JobSpec{Machine: m, Repo: "app", SHA: sha, Lane: "x", Slot: "h1",
		Argv: []string{"sh", "-c", `echo "${#HQ_BIG_A} ${#HQ_BIG_B} ${#HQ_BIG_C}"`}, Env: env})
	if want := "61440 61440 61440"; rc != 0 || strings.TrimSpace(out) != want {
		t.Fatalf("rc=%d out=%q, want %q\n%s", rc, out, want, errs)
	}
	assertNoWorktrees(t, m)
}

// A name that is not a variable name, or one of the job's own HEAVYQ_ inputs, is refused before anything
// starts: it would otherwise inject shell code or steer the job.
func TestRemoteJobRefusesAnIdentityVariableItCannotPass(t *testing.T) {
	m, sha := shippedApp(t)
	for _, spec := range []JobSpec{
		{Env: []string{"HEAVYQ_BASE_REF=x"}},
		{Env: []string{"A;touch x=1"}},
		{Unset: []string{"HEAVYQ_CMD_LEN"}},
	} {
		spec.Machine, spec.Repo, spec.SHA, spec.Lane, spec.Slot, spec.Argv = m, "app", sha, "x", "h1", []string{"true"}
		if _, err := StartJob(spec); err == nil || !strings.Contains(err.Error(), "cannot pass the variable") {
			t.Errorf("env %v unset %v: err = %v, want a refusal", spec.Env, spec.Unset, err)
		}
	}
}

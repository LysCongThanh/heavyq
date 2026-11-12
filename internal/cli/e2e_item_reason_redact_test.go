package cli

import (
	"strings"
	"testing"
)

// The reason an item is not proven names its proof command. item ls --json
// and item done show it, so the command in it is redacted like proof.value:
// no output of either ever holds the secret.
func TestE2EItemReasonIsRedacted(t *testing.T) {
	const secret = "hunter2hunter2"
	h := newRig(t, baseConfig)
	anchor := "# Goal: redaction\n\n" +
		"- [ ] never-run: not run yet `proof: true --token=" + secret + "`\n" +
		"- [ ] red: fails `proof: false --token=" + secret + "`\n"
	app, _ := itemRepo(t, h, anchor, "", nil)

	noSecret := func(what, out string) {
		t.Helper()
		if strings.Contains(out, secret) {
			t.Errorf("%s holds the secret:\n%s", what, out)
		}
	}
	rc, out, errOut := h.inDir(app, nil, "item", "ls", "--all", "--json")
	if rc != 0 || !strings.Contains(out, "not run on this code yet: true --token=***") {
		t.Fatalf("item ls rc=%d\n%s%s", rc, out, errOut)
	}
	noSecret("item ls --json", out+errOut)

	rc, out, errOut = h.inDir(app, nil, "item", "done", "never-run")
	if rc != 1 || !strings.Contains(errOut, "not run on this code yet: true --token=***") {
		t.Fatalf("item done rc=%d\n%s%s", rc, out, errOut)
	}
	noSecret("item done", out+errOut)

	rc, out, errOut = h.inDir(app, nil, "item", "done", "red", "--run")
	if rc != 1 || !strings.Contains(errOut, "failed on this code: false --token=***") {
		t.Fatalf("item done --run rc=%d\n%s%s", rc, out, errOut)
	}
	noSecret("item done --run", out+errOut)

	rc, out, errOut = h.inDir(app, nil, "item", "ls", "--all", "--json")
	if rc != 0 || !strings.Contains(out, "failed on this code: false --token=***") {
		t.Fatalf("item ls after the failure rc=%d\n%s%s", rc, out, errOut)
	}
	noSecret("item ls --json after the failure", out+errOut)
}

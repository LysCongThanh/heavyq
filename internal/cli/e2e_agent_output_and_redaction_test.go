package cli

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// hook.log keeps the rewritten command line; a secret in it is masked like
// everywhere else heavyq shows a command line.
func TestE2EHookLogRedactsTheRewrittenCommand(t *testing.T) {
	h := newRig(t, baseConfig)
	gitRepo(t, h)
	secret := "ghp_" + strings.Repeat("a1B2", 9)
	if rc, out, errOut := h.runStdin(hookJSON(h.cwd, "GITHUB_TOKEN="+secret+" go test ./..."), "hook", "claude-pretooluse"); rc != 0 || errOut != "" || out == "" {
		t.Fatalf("rc=%d out=%q err=%q", rc, out, errOut)
	}
	log := h.read("hook.log")
	if !strings.Contains(log, "rewritten: ") || strings.Contains(log, secret) {
		t.Fatalf("hook.log must hold the rewrite, redacted:\n%s", log)
	}
}

// Agent mode: the command line heavyq quotes (running line, digest header,
// why) never carries a control byte from argv: no ANSI, no carriage return,
// one line each.
func TestE2EAgentLinesCarryNoControlBytesFromTheCommand(t *testing.T) {
	h := newAgentRig(t)
	script := "true # first\n# second \x1b[31mred\x1b[0m \rcr"
	rc, out := h.run("3", "--", "sh", "-c", script)
	if rc != 0 {
		t.Fatalf("rc=%d\n%q", rc, out)
	}
	for _, l := range heavyqLines(out) {
		if strings.ContainsAny(l, "\x1b\r\t") {
			t.Fatalf("control bytes in %q", l)
		}
	}
	if n := len(heavyqLines(out)); n != 2 || strings.Count(out, "\n") != 2 {
		t.Fatalf("a run is the running line and the digest, one line each:\n%q", out)
	}
}

// The refusal of `heavyq trust` under a coding agent does not depend on how
// the person likes heavyq's output: HEAVYQ_OUTPUT=human or HEAVYQ_AGENT=0
// changes the wording, not who is running the command.
func TestE2ETrustIsRefusedUnderAnAgentWhateverTheOutputMode(t *testing.T) {
	h := newRig(t, baseConfig)
	gitRepo(t, h)
	for _, override := range []string{"HEAVYQ_OUTPUT=human", "HEAVYQ_AGENT=0"} {
		cmd := exec.Command(binPath, "trust")
		cmd.Dir, cmd.Env = h.cwd, append(append([]string{}, h.env...), "CLAUDECODE=1", override)
		var eb bytes.Buffer
		cmd.Stderr = &eb
		if rc := processExit(cmd.Run()); rc != 1 || !strings.Contains(eb.String(), "trust is for a person") {
			t.Fatalf("%s: rc=%d %q", override, rc, eb.String())
		}
	}
}

// A need written with `item block` is free text that may quote a command line
// with a secret: every place that shows it masks it, as notifications do.
func TestE2EBlockedItemNeedsAreRedactedEverywhere(t *testing.T) {
	h := newRig(t, baseConfig)
	app, _ := itemRepo(t, h, itemAnchor, "", nil)
	secret := "sk-" + "live-abc123def456"
	if rc, _, errOut := h.inDir(app, nil, "item", "claim", "web-item"); rc != 0 {
		t.Fatalf("claim rc=%d\n%s", rc, errOut)
	}
	if rc, _, errOut := h.inDir(app, nil, "item", "block", "web-item", "needs: deploy with API_KEY="+secret); rc != 0 {
		t.Fatalf("block rc=%d\n%s", rc, errOut)
	}
	for _, args := range [][]string{{"item", "ls"}, {"status", "--json"}, {"why"}, {"brief"}, {"brief", "--json"}} {
		_, out, errOut := h.inDir(app, []string{"CLAUDECODE=1"}, args...)
		if strings.Contains(out+errOut, secret) {
			t.Fatalf("heavyq %s shows the secret:\n%s%s", strings.Join(args, " "), out, errOut)
		}
	}
}

// An agent's run prints at most queued, running and the digest. The note about
// a `sh -c` pipe goes into the digest, and a remote job does not add a banner
// of its own.
func TestE2EAgentRunStaysInItsLineBudget(t *testing.T) {
	h := newAgentRig(t)
	h.setConfig(remoteConfig(h, ""))
	gitRepo(t, h)
	rc, out := h.run("3", "--remote", "local", "--", "sh", "-c", `printf "a\nb\n" | tail -1`)
	if lines := heavyqLines(out); rc != 0 || len(lines) != 2 || !strings.Contains(out, "pipe") {
		t.Fatalf("a pipe in sh -c: rc=%d, want the running line and a digest that mentions the pipe:\n%s", rc, out)
	}
	rc, out = h.run("3", "--remote", "box", "--", "true")
	if rc != 0 || strings.Contains(out, "heavyq-job:") || len(heavyqLines(out)) != 2 {
		t.Fatalf("a remote run: rc=%d\n%s", rc, out)
	}
}

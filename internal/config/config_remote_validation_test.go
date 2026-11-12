package config

import (
	"bufio"
	"bytes"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The default transport must notice a silently dropped link (power loss, an
// expired NAT/VPN mapping): without ServerAlive* the job's ssh waits for TCP
// keepalive, about two hours, holding the ticket. (The remote side is freed by
// the remote sshd's ClientAlive*, which heavyq cannot set from here.) The
// options are checked by ssh itself (ssh -G parses them without connecting;
// -F /dev/null keeps the operator's ~/.ssh/config out of it).
func TestDefaultTransportDetectsADeadLinkWithinAMinuteOrTwo(t *testing.T) {
	c, err := Load(writeConfig(t, "[[remote]]\nname = \"box\"\nssh = \"box\"\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := c.Machine("box")
	if len(r.Transport) < 2 || r.Transport[0] != "ssh" || r.Transport[len(r.Transport)-1] != "box" {
		t.Fatalf("transport = %q", r.Transport)
	}
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("no ssh client on this machine")
	}
	args := append([]string{"-G", "-F", "/dev/null"}, r.Transport[1:]...)
	out, err := exec.Command("ssh", args...).Output()
	if err != nil {
		t.Fatalf("ssh -G %v: %v", args, err)
	}
	opts := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), " "); ok {
			opts[k] = v
		}
	}
	interval, _ := strconv.Atoi(opts["serveraliveinterval"])
	count, _ := strconv.Atoi(opts["serveralivecountmax"])
	if opts["batchmode"] != "yes" || interval <= 0 || count <= 0 {
		t.Fatalf("default transport has no keepalive: %v", opts)
	}
	if dead := time.Duration(interval*count) * time.Second; dead > 2*time.Minute {
		t.Fatalf("a dead link is noticed only after %v", dead)
	}
}

// work_dir "~/x" must mean what git push's "~/" means (the remote home), or
// the scripts create the bare repo in "$HOME/~/x" while the push goes to
// "$HOME/x" and every run exits 77.
func TestRemoteWorkDirTildeIsTheRemoteHome(t *testing.T) {
	c, err := Load(writeConfig(t, "[[remote]]\nname = \"b\"\nssh = \"b\"\nwork_dir = \"~/.hq\"\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := c.Machine("b")
	if r.WorkDir != ".hq" || r.GitURL != "b:.hq/git" {
		t.Fatalf("work_dir=%q git_url=%q, want .hq and b:.hq/git", r.WorkDir, r.GitURL)
	}
	for _, bad := range []string{"~", "~/", "~other/x", "~x"} {
		body := "[[remote]]\nname = \"b\"\nssh = \"b\"\nwork_dir = \"" + bad + "\"\n"
		if _, err := Load(writeConfig(t, body), true); err == nil || !strings.Contains(err.Error(), "work_dir") {
			t.Errorf("work_dir %q: err = %v", bad, err)
		}
	}
}

// A remote named "auto" loads but can never be targeted: every resolver reads
// "auto" as the scheduler keyword first.
func TestRemoteNameAutoIsReserved(t *testing.T) {
	_, err := Load(writeConfig(t, "[[remote]]\nname = \"auto\"\nssh = \"box\"\n"), true)
	if err == nil || !strings.Contains(err.Error(), `"auto"`) {
		t.Fatalf("err = %v", err)
	}
}

// git push reads a URL that starts with '-' as an option. ssh builds the
// default git_url even with a custom transport, so it is checked too.
func TestRemoteGitURLAndSSHCannotBeOptions(t *testing.T) {
	cases := map[string]string{
		"ssh with transport": "[[remote]]\nname = \"b\"\ntransport = [\"ssh\", \"b\"]\nssh = \"-oProxyCommand=x\"\n",
		"git_url option":     "[[remote]]\nname = \"b\"\nssh = \"b\"\ngit_url = \"--receive-pack=touch x\"\n",
		"git_url newline":    "[[remote]]\nname = \"b\"\nssh = \"b\"\ngit_url = \"b:x\\n--y\"\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, body), true); err == nil {
				t.Fatalf("accepted %q", body)
			}
		})
	}
	ok := "[[remote]]\nname = \"b\"\ntransport = [\"sh\", \"-c\"]\ngit_url = \"/srv/heavyq/git\"\n"
	if _, err := Load(writeConfig(t, ok), true); err != nil {
		t.Fatalf("plain path git_url refused: %v", err)
	}
}

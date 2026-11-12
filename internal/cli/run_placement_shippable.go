package cli

import (
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// unshippable says why a commit of the repository at repo cannot be shipped
// to a remote at all, or "" when it can. The remote side is SHA-1 (its bare
// repo, the job's 40-hex checks), and its bare repo refuses a push from a
// shallow clone. A repository git cannot describe counts as unshippable: the
// check fails closed.
func unshippable(repo string) string {
	out, err := exec.Command("git", "-C", repo, "rev-parse", "--is-shallow-repository", "--show-object-format").Output()
	f := strings.Fields(string(out))
	switch {
	case err != nil || len(f) != 2:
		return "git cannot tell the repository's object format (git 2.25 or later is needed)"
	case f[1] != "sha1":
		return "this repository uses " + f[1] + " object names and remote runs need SHA-1"
	case f[0] == "true":
		return "this clone is shallow and remote runs need its history (git fetch --unshallow)"
	}
	return ""
}

// narrowToShippable keeps a run whose repository cannot be shipped off the
// remotes: a target that may run here (default placement, auto, a list with
// local) becomes local, and a remote-only target is an error (exit 2) rather
// than a ship that can only fail (77).
func (p *placement) narrowToShippable(repo string, stderr io.Writer) error {
	if p.target == config.LocalName || repo == "" {
		return nil
	}
	why := unshippable(repo)
	if why == "" {
		return nil
	}
	if !config.TargetMayRunLocally(p.target) {
		return fmt.Errorf("remote runs need a shippable repository: %s (use --remote local)", why)
	}
	if !p.defaultMode {
		fmt.Fprintf(stderr, "heavyq: %s => this machine only\n", why)
	}
	p.target, p.defaultMode = config.LocalName, false
	return nil
}

package cli

import (
	"fmt"
	"io"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
)

// Where a proof's machine came from, for messages.
const (
	proofFromFlag    = "--remote"
	proofFromGate    = "the gate's remote"
	proofFromRepo    = "[proof] remote in " + repoconfig.FileName
	proofFromUser    = "[proof] remote in the user config"
	proofFromDefault = "the default"
)

// proofPlace is the machine set a proof runs on and why.
type proofPlace struct {
	Remote string // local | auto | <remote name> | an ordered list "a,b"
	From   string // one of the proofFrom* constants
}

// resolveProofRemote says where a proof runs. Most specific first:
//
//	--remote flag > the gate's own remote key > [proof] remote of heavyq.toml >
//	[proof] remote of the user config > local (the default).
//
// gate is nil for a command proof (no gate involved). repo may be nil (no
// heavyq.toml). A remote that is not local, auto or a configured [[remote]]
// is an error that names where it was set.
func resolveProofRemote(cfg *config.Config, repo *repoconfig.Repo, gate *repoconfig.Gate, flagVal string) (proofPlace, error) {
	p := proofPlace{Remote: config.LocalName, From: proofFromDefault}
	switch {
	case flagVal != "":
		p = proofPlace{flagVal, proofFromFlag}
	case gate != nil && gate.RemoteSet:
		p = proofPlace{gate.Remote, proofFromGate}
	case repo != nil && repo.Proof.Remote != "":
		p = proofPlace{repo.Proof.Remote, proofFromRepo}
	case cfg.Proof.Remote != "":
		p = proofPlace{cfg.Proof.Remote, proofFromUser}
	}
	if p.From == proofFromGate {
		return p, nil // an unknown remote of a gate fails when the command runs, as it always did
	}
	if err := cfg.CheckRemoteTarget(p.Remote); err != nil {
		return p, fmt.Errorf("%s: %w", p.From, err)
	}
	return p, nil
}

// fitProofToTree decides whether a proof placed on pl can count for the code
// in root. A remote runs the committed code only, so its result proves the
// work tree only when the tree is exactly HEAD's. With uncommitted changes:
//
//   - auto, or a list that names local, falls back to local, the only machine
//     the edits exist on (the same rule as default placement: dirty repo,
//     stays local);
//   - a named remote, or a list of remotes only, is refused: the owner asked
//     to keep this off the main machine, and a run there would be proof of
//     code nobody is holding.
//
// A gate's own remote key is left alone (it predates [proof]). refusal is a
// message for the caller to print ("" when the placement stands).
func fitProofToTree(root string, paths []string, pl proofPlace, stderr io.Writer) (proofPlace, string) {
	// A gate's own remote key predates [proof] and keeps its behaviour.
	if pl.Remote == config.LocalName || pl.From == proofFromGate || committedTreeIsWorkTree(root, paths) {
		return pl, ""
	}
	if config.TargetMayRunLocally(pl.Remote) {
		fmt.Fprintf(stderr, "heavyq: uncommitted changes: the proof runs locally (%s=%s only places committed code; commit to use a remote)\n", pl.From, pl.Remote)
		return proofPlace{config.LocalName, pl.From}, ""
	}
	return pl, fmt.Sprintf("the proof is set to run on remote %s (%s), which runs committed code only, and this work tree has uncommitted changes: commit them, or add --remote local", pl.Remote, pl.From)
}

// committedTreeIsWorkTree reports whether the code a remote would run (HEAD)
// is exactly the code in the work tree at root: the whole tree, or for a gate that declares paths, the files
// they name (an edit elsewhere is not code the proof depends on). Any doubt is "no".
func committedTreeIsWorkTree(root string, paths []string) bool {
	here, err := gitutil.IdentifyScoped(root, paths)
	if err != nil || here.Tree == "" {
		return false
	}
	pinned, err := gitutil.PinnedScoped(root, "HEAD", paths)
	return err == nil && pinned.Tree == here.Tree
}

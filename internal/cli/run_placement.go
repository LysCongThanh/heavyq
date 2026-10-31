package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/schedule"
)

var subdirRe = regexp.MustCompile(`^[A-Za-z0-9._/-]*$`)

// placement is where a ticket may run and what it ships when remote.
type placement struct {
	target      string // local | auto | <remote>
	defaultMode bool   // auto chosen implicitly: re-verify safety before going remote
	wd          string
	repo        string // absolute repo root (remote/auto only)
	sha         string
	subdir      string
	baseSHA     string
	siblings    []gitutil.Sibling
	refIsHead   bool
	localWhy    string // non-empty: local is not an eligible machine
	candidates  []string
}

// preparePlacement resolves the target machine set for o.
func preparePlacement(cfg *config.Config, o *runOptions, stderr io.Writer) (*placement, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	p := &placement{target: o.Remote, wd: wd, refIsHead: o.Ref == "HEAD"}
	if p.target == "" {
		p.target = "local"
		if cfg.AutoEnabled() {
			if repo, why := autoEligible(wd, o); why == "" {
				p.target, p.defaultMode, o.Repo = "auto", true, repo
			}
		}
	}
	mainRepo := o.Repo
	if mainRepo == "" {
		mainRepo, _ = gitutil.TopLevel(wd)
	}
	if mainRepo != "" {
		if mainRepo, err = filepath.Abs(mainRepo); err != nil {
			return nil, err
		}
	}
	if p.target != "local" {
		if err := p.resolveRemoteSource(mainRepo, o, stderr); err != nil {
			return nil, err
		}
	}
	for _, spec := range o.With {
		s, err := gitutil.ResolveSibling(spec, mainRepo)
		if err != nil {
			return nil, err
		}
		for _, x := range p.siblings {
			if x.Name == s.Name {
				return nil, fmt.Errorf("--with %q given twice", s.Name)
			}
		}
		p.siblings = append(p.siblings, s)
	}
	if p.target == "local" && o.Ref != "HEAD" {
		if p.sha, err = gitutil.ResolveCommit(mainRepo, o.Ref); err != nil {
			return nil, err
		}
		p.repo = mainRepo
	}
	p.refreshLocal()
	if p.target == "local" && p.localWhy != "" {
		return nil, fmt.Errorf("cannot run locally: %s (commit/checkout, or run remotely with --remote)", p.localWhy)
	}
	p.candidates = p.machines(cfg)
	if p.target == "auto" && p.localWhy != "" {
		fmt.Fprintf(stderr, "heavyq: %s => remote machines only\n", p.localWhy)
	}
	return p, nil
}

func (p *placement) resolveRemoteSource(mainRepo string, o *runOptions, stderr io.Writer) error {
	if mainRepo == "" {
		return fmt.Errorf("remote runs need a git repository (run inside one or pass --repo)")
	}
	if !config.SafeName.MatchString(filepath.Base(mainRepo)) {
		return fmt.Errorf("repo directory name %q has unsafe characters for a remote run", filepath.Base(mainRepo))
	}
	wd := p.wd
	if real, err := filepath.EvalSymlinks(wd); err == nil {
		wd = real // git reports resolved paths
	}
	if real, err := filepath.EvalSymlinks(mainRepo); err == nil {
		mainRepo = real
	}
	sha, err := gitutil.ResolveCommit(mainRepo, o.Ref)
	if err != nil {
		return err
	}
	p.repo, p.sha = mainRepo, sha
	if rel, err := filepath.Rel(mainRepo, wd); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		if !subdirRe.MatchString(rel) {
			return fmt.Errorf("current subdirectory %q has unsafe characters for a remote run", rel)
		}
		p.subdir = rel
	}
	if o.BaseRef != "" {
		if b, err := gitutil.ResolveCommit(mainRepo, "refs/remotes/"+o.BaseRef); err == nil {
			p.baseSHA = b
		} else {
			fmt.Fprintf(stderr, "heavyq: note: %s has no refs/remotes/%s; the remote worktree will not have it\n", filepath.Base(mainRepo), o.BaseRef)
			o.BaseRef = ""
		}
	}
	return nil
}

// refreshLocal re-checks whether the local work tree is the code asked for:
// local runs execute in place, so a --ref other than HEAD or a --with sibling
// not at its pinned commit (or dirty) makes local ineligible.
func (p *placement) refreshLocal() {
	var why []string
	if !p.refIsHead && p.sha != "" {
		if head, err := gitutil.ResolveCommit(p.repo, "HEAD"); err != nil || head != p.sha {
			why = append(why, fmt.Sprintf("--ref %.12s is not the checked-out HEAD", p.sha))
		}
	}
	for _, s := range p.siblings {
		if m := s.LocalMismatch(); m != "" {
			why = append(why, m)
		}
	}
	p.localWhy = strings.Join(why, "; ")
}

func (p *placement) machines(cfg *config.Config) []string {
	switch p.target {
	case "local":
		return []string{config.LocalName}
	case "auto":
		var out []string
		if p.localWhy == "" {
			out = append(out, config.LocalName)
		}
		for _, r := range cfg.Remotes {
			out = append(out, r.Name)
		}
		return out
	default:
		return []string{p.target}
	}
}

// autoEligible reports the repo root when a default ticket may be placed
// remotely: inside a clean git repo and the command references nothing
// outside it. Otherwise it returns the reason it stays local.
func autoEligible(wd string, o *runOptions) (string, string) {
	dir := wd
	if o.Repo != "" {
		dir = o.Repo
	}
	top, err := gitutil.TopLevel(dir)
	if err != nil {
		return "", "not in a git repository"
	}
	if !config.SafeName.MatchString(filepath.Base(top)) {
		return "", "repo directory name has unsafe characters"
	}
	if !gitutil.IsClean(top) {
		return "", "uncommitted changes"
	}
	if why := schedule.OutsideRepoReference(o.Argv, top); why != "" {
		return "", "command " + why
	}
	return top, ""
}

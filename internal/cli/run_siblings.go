package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/gitutil"
)

// resolveSiblings pins every --with spec to a commit now, at enqueue time.
// Any bad spec fails the whole run (exit 2, nothing ran): a missing directory,
// a non-repository, an unknown ref, an unsafe or main-repo name, or two specs
// that would land on the same directory name.
func resolveSiblings(specs []string, mainRepo string) ([]gitutil.Sibling, error) {
	var out []gitutil.Sibling
	for _, spec := range specs {
		s, err := gitutil.ResolveSibling(spec, mainRepo)
		if err != nil {
			return nil, err
		}
		for _, x := range out {
			if x.Name == s.Name {
				return nil, fmt.Errorf("--with %q: %q is already given by another --with (declared twice)", spec, s.Name)
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// siblingIDs are the "name@sha" strings the run identity and the remote job
// take: the full pinned commit, never an abbreviation.
func siblingIDs(sibs []gitutil.Sibling) []string {
	var out []string
	for _, s := range sibs {
		out = append(out, s.Name+"@"+s.SHA)
	}
	return out
}

// siblingLabels are the "name@sha12" strings status and history show.
func siblingLabels(sibs []gitutil.Sibling) []string {
	var out []string
	for _, s := range sibs {
		out = append(out, s.Label())
	}
	return out
}

// argvWithoutSiblingPaths hides "../<name>" for each declared sibling from the
// outside-the-repo check of default placement: the run has that directory on
// every machine it may go to, so naming it is not a reason to stay local.
// Anything else outside the repository (other "..", ~, absolute paths) stays
// visible to the check.
func argvWithoutSiblingPaths(argv []string, sibs []gitutil.Sibling) []string {
	if len(sibs) == 0 {
		return argv
	}
	out := make([]string, len(argv))
	copy(out, argv)
	for _, s := range sibs {
		re := regexp.MustCompile(`\.\./` + regexp.QuoteMeta(s.Name) + "(/|$|[\\s\"'`;)])")
		for i := range out {
			out[i] = re.ReplaceAllString(out[i], "@SIBLING@$1")
		}
	}
	return out
}

// printWith adds the "with: name@sha12 ..." line under a ticket of `status`.
func printWith(w io.Writer, labels []string) {
	if len(labels) > 0 {
		fmt.Fprintf(w, "      with: %s\n", strings.Join(labels, " "))
	}
}

// siblingDrift says which --with sibling is no longer the pinned, clean
// checkout a local command was admitted on ("" when none moved). Remote runs
// use the shipped commits, so only a local result can be spoiled this way.
func (r *run) siblingDrift() string {
	for _, s := range r.pl.siblings {
		if m := s.LocalMismatch(); m != "" {
			return m
		}
	}
	return ""
}

// mainRepoRoot is the root of the repository the run is about: the one that
// holds repo (the --repo value) or the cwd. "" when there is none (a local run
// outside git); a --repo that is no repository is kept as given, absolute.
func mainRepoRoot(wd, repo string) (string, error) {
	dir := wd
	if repo != "" {
		abs, err := filepath.Abs(repo)
		if err != nil {
			return "", err
		}
		dir = abs
	}
	if top, err := gitutil.TopLevel(dir); err == nil {
		return top, nil
	}
	if repo != "" {
		return dir, nil
	}
	return "", nil
}

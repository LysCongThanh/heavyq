package cli

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// pinToThisMachineForGH applies --need-gh: the command calls the GitHub CLI
// with the login only this machine has, so the run is placed locally. The
// flag is also implied for the repositories listed in [run] need_gh_repos
// (one stderr line says so). Asking for a named remote as well is a usage
// error, never a guess; "auto", or a list that names local, simply narrows
// to local.
func (o *runOptions) pinToThisMachineForGH(cfg *config.Config, mainRepo string, stderr io.Writer) error {
	repoDir := filepath.Base(mainRepo)
	implied := false
	if !o.NeedGH && mainRepo != "" && cfg.NeedsGH(repoDir) {
		o.NeedGH, implied = true, true
	}
	if !o.NeedGH {
		return nil
	}
	if o.Remote != "" && !config.TargetMayRunLocally(o.Remote) {
		if implied {
			return fmt.Errorf("%s is in [run] need_gh_repos, so its runs need this machine's GitHub login and --remote %s is not possible (drop --remote, or remove the repo from need_gh_repos)", repoDir, o.Remote)
		}
		return fmt.Errorf("--need-gh needs this machine's GitHub login, so --remote %s is not possible (use local)", o.Remote)
	}
	o.Remote = config.LocalName
	if implied {
		fmt.Fprintf(stderr, "heavyq: %s is in [run] need_gh_repos (needs this machine's GitHub login): running locally\n", repoDir)
	}
	return nil
}

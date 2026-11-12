package cli

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
	"github.com/LysCongThanh/heavyq/internal/toolcheck"
)

// printRemoteTools is the remote tools check of `heavyq doctor`: for every
// [[remote]], which of the common tools (toolcheck.Common) and of the tools the
// gates of the current repository's heavyq.toml declare resolve on the PATH a
// job gets there. Every remote is asked now, in parallel, and the answers
// replace what placement had cached. Informational: it never changes doctor's
// exit code, and prints nothing without remotes or when the config does not
// load (the config check already said why).
func printRemoteTools(w io.Writer, cfgPath string, explicit bool) {
	cfg, err := config.Load(cfgPath, explicit)
	if err != nil || len(cfg.Remotes) == 0 {
		return
	}
	declared := declaredGateTools()
	tools := toolcheck.Union(toolcheck.Common, declared)
	var names []string
	for _, r := range cfg.Remotes {
		names = append(names, r.Name)
	}
	answers, errs := remoteToolAnswers(cfg, names, tools, nil, true)
	fmt.Fprintln(w, "remote tools (on the PATH a job gets there):")
	short := false
	for _, n := range names {
		if err := errs[n]; err != nil {
			msg, _, _ := strings.Cut(err.Error(), "\n")
			fmt.Fprintf(w, "  %s: not asked: %s\n", n, msg)
			continue
		}
		var found, missing []string
		for _, t := range tools {
			if a, ok := answers[n][t]; ok && a.Found {
				found = append(found, t)
			} else {
				missing = append(missing, t)
				short = short || slices.Contains(declared, t)
			}
		}
		line := "  " + n + ": " + orDefault(strings.Join(found, ", "), "(none)")
		if len(missing) > 0 {
			line += " | missing: " + strings.Join(missing, ", ")
		}
		fmt.Fprintln(w, line)
	}
	if short {
		fmt.Fprintf(w, "  a remote lacks a tool heavyq.toml declares: runs that need it skip that remote; add the tool's directory to its env_path in %s\n", cfgName(cfg))
	}
}

// declaredGateTools are the tools the gates of the current repository's
// heavyq.toml declare (none outside a repository or without the file; an
// invalid file is reported by the commands that use it).
func declaredGateTools() []string {
	wd, err := os.Getwd()
	if err != nil {
		return nil
	}
	repo, err := repoconfig.Load(wd)
	if err != nil || repo == nil {
		return nil
	}
	var out []string
	for _, g := range repo.Gates {
		out = toolcheck.Union(out, g.Tools)
	}
	return out
}

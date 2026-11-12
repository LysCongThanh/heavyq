package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/LysCongThanh/heavyq/internal/agentmode"
	"github.com/LysCongThanh/heavyq/internal/gitutil"
	"github.com/LysCongThanh/heavyq/internal/stance"
)

const trustUsage = `usage:
  heavyq trust             trust this repository's .heavyq/stances as they are now
  heavyq trust --list      list trusted repositories and whether they still match
  heavyq trust --revoke    forget this repository

A repository's own stances are text that heavyq injects into an agent's context, so they only count
once you have read them and run "heavyq trust" yourself (like direnv allow). The trust is a content
hash: any edit, addition or removal of a stance file asks again. Your own stances and the built-in
ones need no trust. Run it from your terminal, not from an agent.
`

// trustCommand implements `heavyq trust [--list|--revoke]`.
func trustCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("trust", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, trustUsage) }
	list := fs.Bool("list", false, "list trusted repositories")
	revoke := fs.Bool("revoke", false, "forget this repository")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || (*list && *revoke) {
		fs.Usage()
		return ExitUsage
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "heavyq:", err)
		return ExitUsage
	}
	env := stance.DefaultEnv(cwd)
	if env.Trust.Path == "" {
		fmt.Fprintln(stderr, "heavyq: no state directory for the trust list ($XDG_STATE_HOME or $HOME)")
		return 1
	}
	if *list {
		return trustList(env, stdout, stderr)
	}
	common, err := gitutil.CommonDir(cwd)
	if err != nil {
		fmt.Fprintln(stderr, "heavyq: trust needs a git repository")
		return ExitUsage
	}
	if *revoke {
		was, err := env.Trust.Revoke(common)
		if err != nil {
			return stanceFail(stderr, err)
		}
		if was {
			fmt.Fprintln(stdout, "no longer trusting the stances of", common)
		} else {
			fmt.Fprintln(stdout, "this repository was not trusted")
		}
		return 0
	}
	return trustGrant(env, common, stdout, stderr)
}

func trustGrant(env stance.Env, common string, stdout, stderr io.Writer) int {
	// Trust is a human decision: a stance file may be written to talk an agent
	// into running this very command. Refuse under a coding agent: its own
	// variables decide, not HEAVYQ_OUTPUT / HEAVYQ_AGENT, which only shape output
	// (an agent can set them, and a person may export them for every shell).
	if mode, _ := agentmode.Resolve(os.LookupEnv); mode.Terse() || agentmode.UnderAgent(os.LookupEnv) {
		fmt.Fprintln(stderr, "heavyq: trust is for a person: read .heavyq/stances and run heavyq trust from your own terminal")
		return 1
	}
	hash, names, err := stance.ContentHash(env.Dirs.Repo)
	if err != nil {
		return stanceFail(stderr, err)
	}
	if hash == "" {
		fmt.Fprintln(stdout, "this repository has no stances in .heavyq/stances: nothing to trust")
		return 0
	}
	if _, err := stance.Load(env.Dirs); err != nil {
		fmt.Fprintln(stderr, "heavyq: invalid stance file(s), not trusting them:")
		fmt.Fprintln(stderr, err)
		return ExitUsage
	}
	if err := env.Trust.Grant(common, hash, time.Now()); err != nil {
		return stanceFail(stderr, err)
	}
	fmt.Fprintf(stdout, "trusting %d stance file(s) in %s: %v\n  %s\nan edit asks again\n", len(names), env.Dirs.Repo, names, hash)
	return 0
}

func trustList(env stance.Env, stdout, stderr io.Writer) int {
	entries, err := env.Trust.Entries()
	if err != nil {
		return stanceFail(stderr, err)
	}
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "no trusted repositories")
		return 0
	}
	for _, repo := range stance.SortedRepos(entries) {
		fmt.Fprintf(stdout, "%s  %s  trusted %s\n", repo, entries[repo].Hash, entries[repo].At.Local().Format("2006-01-02 15:04"))
	}
	return 0
}

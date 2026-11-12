package cli

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/agentmode"
	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/proof"
	"github.com/LysCongThanh/heavyq/internal/toolcheck"
)

// addTools adds the names of one --tools value (the flag repeats) to the run's tools.
func (o *runOptions) addTools(v string) error {
	ts, err := toolcheck.ParseList(v)
	if err == nil {
		err = toolcheck.CheckList(append(slices.Clone(o.Tools), ts...))
	}
	if err != nil {
		return fmt.Errorf("--tools %q: %w", v, err)
	}
	o.Tools = toolcheck.Union(o.Tools, ts)
	return nil
}

// placeByTools leaves out of the candidates every remote that answered it does
// not have a tool the run needs: the commands its argv starts
// (toolcheck.FromArgv) and --tools. It runs once, before the ticket is queued
// and after the metrics probe: only remotes that probe reached are asked, the
// others answer from the cache or not at all, and a remote whose answer is
// unknown stays a candidate. A run no machine is left for is refused before any
// code is shipped, after the remotes whose cached answer refused it were asked
// again (the tool may have been installed meanwhile).
func (r *run) placeByTools() error {
	remotes := remoteCandidates(r.pl.candidates)
	tools := toolcheck.Union(argvTools(r.opts.Argv), r.opts.Tools)
	if len(remotes) == 0 || len(tools) == 0 {
		return nil
	}
	reached := func(n string) bool { _, ok := r.probes.get(n); return ok }
	asked := time.Now()
	answers, _ := remoteToolAnswers(r.cfg, remotes, tools, reached, false)
	r.setLacking(missingTools(answers, tools))
	if len(r.pl.candidates) == 0 && answeredBefore(answers, r.pl.lacking, asked) {
		answers, _ = remoteToolAnswers(r.cfg, remotes, tools, reached, true)
		r.setLacking(missingTools(answers, tools))
	}
	if len(r.pl.candidates) == 0 {
		return lacksError(r.cfg, r.pl.lacking)
	}
	for _, n := range slices.Sorted(maps.Keys(r.pl.lacking)) {
		r.note("%s %s: not placing it there", n, toolcheck.Reason(r.pl.lacking[n]))
	}
	return nil
}

// argvTools are the commands argv starts (toolcheck.FromArgv). The wrapper
// `heavyq gate run` puts around a gate command (proof.Argv) runs that command
// as a shell script, so it is read as `sh -c '<command>'`.
func argvTools(argv []string) []string {
	if g := proof.Argv(""); len(argv) == len(g) && slices.Equal(argv[:len(g)-1], g[:len(g)-1]) {
		argv = []string{"sh", "-c", argv[len(g)-1]}
	}
	return toolcheck.FromArgv(argv)
}

// setLacking records which remotes lack a tool and recomputes the candidates.
func (r *run) setLacking(lacking map[string][]string) {
	r.pl.lacking = lacking
	r.pl.candidates = r.pl.machines()
}

// answeredBefore reports whether a "missing" answer behind lacking came from
// the cache, i.e. was given before this placement asked at t.
func answeredBefore(answers map[string]map[string]toolcheck.Answer, lacking map[string][]string, t time.Time) bool {
	for n, tools := range lacking {
		for _, tool := range tools {
			if answers[n][tool].At.Before(t) {
				return true
			}
		}
	}
	return false
}

// withoutLacking drops the machines that lack a tool the run needs.
func (p *placement) withoutLacking(names []string) []string {
	if len(p.lacking) == 0 {
		return names
	}
	return slices.DeleteFunc(names, func(n string) bool { _, lacks := p.lacking[n]; return lacks })
}

// lacksSummary names each remote with the tools it lacks and its env_path, where that is fixed.
func lacksSummary(cfg *config.Config, lacking map[string][]string) string {
	var parts []string
	for _, n := range slices.Sorted(maps.Keys(lacking)) {
		m, _ := cfg.Machine(n)
		parts = append(parts, fmt.Sprintf("%s lacks %s (env_path %q)", n, strings.Join(lacking[n], ", "), m.EnvPath))
	}
	return strings.Join(parts, "; ")
}

// lacksError is the refusal of a run that no machine of its target can run.
func lacksError(cfg *config.Config, lacking map[string][]string) error {
	return fmt.Errorf("not run, nothing shipped: %s on the PATH its jobs get; add the tool's directory to that [[remote]]'s "+
		"env_path in %s (`heavyq doctor` lists each remote's tools), or choose another --remote", lacksSummary(cfg, lacking), cfgName(cfg))
}

// nextToolsGap is what an agent does after a run was refused for the tools of its remotes.
const nextToolsGap = "install the tool there or add its directory to the remote's env_path (`heavyq doctor` lists its tools), " +
	"or add --remote local, then re-run"

// toolsFail reports a run refused by placeByTools (exit 2) before it was queued. A person reads the error;
// an agent (or --digest) reads the usual digest: NOT RUN, which remotes lack what, and what to do.
func (r *run) toolsFail(err error) int {
	r.note("%s", err)
	if r.out.digest {
		r.printDigest(agentmode.Result{Verdict: agentmode.NotRun, Cmd: r.cmdText(), Exit: ExitUsage,
			Why: lacksSummary(r.cfg, r.pl.lacking), Next: nextToolsGap})
	}
	return ExitUsage
}

// addLacksReasons puts the reason of each remote a ticket left out for a missing tool into reasons
// (status and why read it from the ticket: nothing is probed for them).
func addLacksReasons(reasons map[string]string, lacks map[string][]string) {
	for n, tools := range lacks {
		reasons[n] = toolcheck.Reason(tools)
	}
}

// printLacks adds a "not on box: lacks x" line per such remote under a waiting ticket of `status`.
func printLacks(w io.Writer, lacks map[string][]string) {
	for _, n := range slices.Sorted(maps.Keys(lacks)) {
		fmt.Fprintf(w, "      not on %s: %s\n", n, toolcheck.Reason(lacks[n]))
	}
}

package remote

import (
	"fmt"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/toolcheck"
)

// The markers around the PATH block of remote_job.sh. The tool probe runs that block as it is, so a tool
// it finds is a tool the job finds: same login PATH on macOS, same env_path first, same "~/" expansion.
const (
	pathBlockBegin = "# >>> job PATH\n"
	pathBlockEnd   = "# <<< job PATH\n"
)

// jobPathScript is the PATH block of the job script (TestJobPathBlockIsCutFromTheJobScript).
var jobPathScript = cutBlock(jobScript, pathBlockBegin, pathBlockEnd)

func cutBlock(s, begin, end string) string {
	_, rest, okBegin := strings.Cut(s, begin)
	block, _, okEnd := strings.Cut(rest, end)
	if !okBegin || !okEnd {
		panic("remote_job.sh: the job PATH markers are missing")
	}
	return block
}

// toolProbeTail answers, for each tool in "$@", "heavyq-tool + <name> <where>" or "heavyq-tool - <name>".
// `command -v` also finds builtins and keywords, which a job's `bash -c` has too.
const toolProbeTail = `for t in "$@"; do
  if p="$(command -v "$t" 2> /dev/null)"; then echo "heavyq-tool + $t $p"; else echo "heavyq-tool - $t"; fi
done
`

// ProbeTools reports where each of tools resolves on m with the PATH a job there gets: name -> path, ""
// when it is not found. A directory of env_path that is relative resolves against the job's worktree, which
// the probe does not have: with one, a tool the probe cannot find is left out of the answer (unknown), not
// answered "". An error means m could not be asked; tools must be plain command names.
func ProbeTools(m config.MachineConfig, tools []string, timeout time.Duration) (map[string]string, error) {
	for _, t := range tools { // names only: how many is the caller's limit (a run's tools plus toolcheck.Common)
		if !toolcheck.Valid(t) {
			return nil, fmt.Errorf("tool probe: %q is not a plain command name", t)
		}
	}
	out, err := Transport(m.Transport).outputEnv(timeout, []string{"HEAVYQ_ENV_PATH=" + m.EnvPath},
		jobPathScript+toolProbeTail, tools...)
	if err != nil {
		return nil, err
	}
	got, err := parseToolProbe(out, tools)
	if err != nil {
		return nil, err
	}
	if relativeEnvPath(m.EnvPath) {
		for t, p := range got {
			if p == "" {
				delete(got, t)
			}
		}
	}
	return got, nil
}

// parseToolProbe reads the probe's answer lines (other output, such as a login banner, is ignored). Every
// tool asked must be answered.
func parseToolProbe(out string, tools []string) (map[string]string, error) {
	said := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, " ", 4)
		switch {
		case len(f) == 4 && f[0] == "heavyq-tool" && f[1] == "+" && f[3] != "":
			said[f[2]] = f[3]
		case len(f) == 3 && f[0] == "heavyq-tool" && f[1] == "-":
			said[f[2]] = ""
		}
	}
	got := make(map[string]string, len(tools))
	for _, t := range tools {
		p, ok := said[t]
		if !ok {
			return nil, fmt.Errorf("tool probe: no answer about %q in %q", t, strings.TrimSpace(out))
		}
		got[t] = p
	}
	return got, nil
}

// relativeEnvPath reports whether env_path has a directory that is neither absolute nor under "~".
func relativeEnvPath(p string) bool {
	for _, d := range strings.Split(p, ":") {
		if d != "" && !strings.HasPrefix(d, "/") && d != "~" && !strings.HasPrefix(d, "~/") {
			return true
		}
	}
	return false
}

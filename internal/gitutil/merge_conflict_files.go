package gitutil

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// MergeConflictFiles are the files the merge m conflicted in: the paths a
// person (or an agent) had to resolve by hand, sorted. An empty result is a
// clean merge.
//
// It asks git to redo the merge of m's two parents (git merge-tree, 2.38 or
// newer). That is exact, also when the resolution took one side whole, which
// leaves no trace in the merge commit itself. It writes the merged tree and
// the conflict-marked blobs into the object store as unreferenced objects,
// which a later gc collects; nothing else of the repository changes. An older
// git reads the merge commit's combined diff instead: only files whose
// resolution differs from both parents show, so it can miss a conflict that was
// settled by taking one side.
func MergeConflictFiles(repo string, m MergeCommit) ([]string, error) {
	if !shaRe.MatchString(m.Parent1) || !shaRe.MatchString(m.Parent2) || !shaRe.MatchString(m.SHA) {
		return nil, fmt.Errorf("git: merge %q has no usable parents", m.SHA)
	}
	cmd := exec.Command("git", "-C", repo, "merge-tree", "--write-tree", "--name-only", "--no-messages", "-z", m.Parent1, m.Parent2)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return nil, nil // exit 0: merged cleanly
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return parseMergeTreeConflicts(stdout.String()), nil
	case errors.As(err, &exit) && exit.ExitCode() == 129:
		return combinedDiffFiles(repo, m.SHA) // git older than 2.38: no --write-tree
	default:
		return nil, fmt.Errorf("git merge-tree: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
}

// parseMergeTreeConflicts reads `merge-tree --write-tree --name-only -z`: the
// tree id, then the conflicted file names, NUL (or newline) separated, ending at
// the first empty field.
func parseMergeTreeConflicts(out string) []string {
	fields := strings.Split(strings.ReplaceAll(out, "\x00", "\n"), "\n")
	var files []string
	for _, f := range fields[1:] { // fields[0] is the id of the merged tree
		if f == "" {
			break
		}
		files = append(files, f)
	}
	sort.Strings(files)
	return files
}

// combinedDiffFiles lists the "diff --cc <path>" headers of a merge commit:
// the files git itself shows as resolved in a way neither parent had.
func combinedDiffFiles(repo, sha string) ([]string, error) {
	out, err := gitRaw(repo, "-c", "core.quotepath=off", "show", "--cc", "--format=", "--no-color", sha)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(line, "diff --cc "); ok {
			files = append(files, p)
		}
	}
	sort.Strings(files)
	return files, nil
}

package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// The section `heavyq init --agents-md` maintains in AGENTS.md and CLAUDE.md.
// Everything between the markers belongs to heavyq and is replaced on the next
// run; the rest of the file is never touched.
const (
	agentsBegin = "<!-- heavyq:begin -->"
	agentsEnd   = "<!-- heavyq:end -->"
	agentsBody  = `## Proving work with heavyq

Before saying a task is done, run ` + "`heavyq gate run ready`" + `.

- Exit 0: the gate is proven on this exact code.
- Exit 1: a command failed or is unproven. Fix it, then run it again.
- Exit 75: the queue is busy. Retry later; never bypass heavyq or run the commands another way.

` + "`heavyq gate status`" + ` shows what is already proven, without running anything.
`
	// What updateAgentDoc and mergeAgentsBlock report.
	outcomeAdded   = "added the heavyq section"
	outcomeUpdated = "updated the heavyq section"
	outcomeCurrent = "already up to date"
	// maxAgentDocBytes bounds the file read and rewritten; a longer one is not
	// an agent instruction file heavyq should be rewriting.
	maxAgentDocBytes = 1 << 20
)

// agentDocs are the files --agents-md maintains at the git root. AGENTS.md is
// created when missing; CLAUDE.md only when the project already has one.
var agentDocs = []struct {
	name   string
	create bool
}{{"AGENTS.md", true}, {"CLAUDE.md", false}}

// updateAgentDocs applies the section to each of agentDocs, reporting what it
// did on stdout and what it refused on stderr. It returns false when any file
// could not be updated.
func updateAgentDocs(root string, stdout, stderr io.Writer) bool {
	ok := true
	for _, d := range agentDocs {
		outcome, err := updateAgentDoc(root, filepath.Join(root, d.name), d.create)
		switch {
		case err != nil:
			fmt.Fprintf(stderr, "heavyq: %s: %v\n", d.name, err)
			ok = false
		case outcome != "":
			fmt.Fprintf(stdout, "%s: %s\n", d.name, outcome)
		}
	}
	return ok
}

// updateAgentDoc puts the heavyq section into the file at path and returns
// what it did ("created" or one of the outcomes above), or "" when the file is
// absent and create is false. A symlink is followed only when its target is inside the repository,
// so a hostile repository cannot point AGENTS.md at a file elsewhere.
func updateAgentDoc(root, path string, create bool) (string, error) {
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		if !create {
			return "", nil
		}
		if err := writeNewFile(path, agentsBlock()+"\n"); err != nil {
			return "", err
		}
		return "created", nil
	} else if err != nil {
		return "", err
	}
	target, err := insideRepo(root, path)
	if err != nil {
		return "", err
	}
	data, mode, err := readAgentDoc(target)
	if err != nil {
		return "", err
	}
	merged, outcome, err := mergeAgentsBlock(string(data))
	if err != nil {
		return "", err
	}
	if outcome == outcomeCurrent {
		return outcome, nil
	}
	if err := replaceFile(target, []byte(merged), mode); err != nil {
		return "", err
	}
	return outcome, nil
}

// insideRepo resolves symlinks in path and returns the file it really is,
// which must be inside root.
func insideRepo(root, path string) (string, error) {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(realRoot, target)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("is a symlink to %s, outside the repository; not touching it", target)
	}
	return target, nil
}

// readAgentDoc reads a regular file of at most maxAgentDocBytes.
func readAgentDoc(path string) ([]byte, os.FileMode, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	switch {
	case err != nil:
		return nil, 0, err
	case !fi.Mode().IsRegular():
		return nil, 0, errors.New("is not a regular file")
	case fi.Size() > maxAgentDocBytes:
		return nil, 0, fmt.Errorf("is larger than %d bytes", maxAgentDocBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxAgentDocBytes+1))
	if err != nil {
		return nil, 0, err
	}
	return data, fi.Mode().Perm(), nil
}

// agentsBlock is the section with its markers, without a trailing newline.
func agentsBlock() string {
	return agentsBegin + "\n" + agentsBody + agentsEnd
}

// mergeAgentsBlock returns doc with the heavyq section in it, and what that
// took. Without markers the section is appended after a blank line. With one
// well-formed pair, what is between them (markers included) is replaced, so
// stale text updates and a current section leaves doc byte for byte as it was.
// Anything else (an unpaired, repeated or reversed marker) is an error: heavyq
// cannot tell which text is its own, so it touches nothing.
func mergeAgentsBlock(doc string) (merged, outcome string, err error) {
	begins, ends := strings.Count(doc, agentsBegin), strings.Count(doc, agentsEnd)
	switch {
	case begins == 0 && ends == 0:
		if doc != "" && !strings.HasSuffix(doc, "\n") {
			doc += "\n"
		}
		if doc != "" {
			doc += "\n"
		}
		return doc + agentsBlock() + "\n", outcomeAdded, nil
	case begins == 1 && ends == 1 && strings.Index(doc, agentsBegin) < strings.Index(doc, agentsEnd):
		from := strings.Index(doc, agentsBegin)
		to := strings.Index(doc, agentsEnd) + len(agentsEnd)
		merged = doc[:from] + agentsBlock() + doc[to:]
		if merged == doc {
			return doc, outcomeCurrent, nil
		}
		return merged, outcomeUpdated, nil
	}
	return "", "", fmt.Errorf("has an unpaired, repeated or reversed %s / %s marker; fix it by hand, then run init again", agentsBegin, agentsEnd)
}

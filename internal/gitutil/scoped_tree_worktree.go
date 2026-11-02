package gitutil

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Git modes a scoped file can have.
const (
	modeFile    = "100644"
	modeExec    = "100755"
	modeSymlink = "120000"
)

// workTreeFiles lists the matching files of the work tree at repo as they are
// on disk now, and whether any of them differs from HEAD.
//
// Tracked files that git says are unchanged keep the id the index holds (no
// read of their content); the changed ones and the untracked ones that are not
// ignored are hashed by git itself, so line-ending and other clean filters
// apply as they would for a commit, and an unmodified checkout gives the same
// ids as the commit it was made from.
func workTreeFiles(repo string, keep func(string) bool) ([]scopedFile, bool, error) {
	head, err := ResolveCommit(repo, "HEAD")
	if err != nil {
		return nil, false, err
	}
	idx, err := trackedIndex(repo, keep)
	if err != nil {
		return nil, false, err
	}
	changed, err := listPaths(repo, keep, "ls-files", "--modified", "-z") // modified or deleted since the index
	if err != nil {
		return nil, false, err
	}
	untracked, err := listPaths(repo, keep, append([]string{"ls-files", "--others", "--exclude-standard", "-z"}, untrackedSpecs(nil)...)...)
	if err != nil {
		return nil, false, err
	}
	// Names that differ from HEAD, staged or not: edits that were staged and
	// then undone by hand are not among them, as in the whole-tree identity.
	diff, err := listPaths(repo, keep, "diff", "--name-only", "--no-renames", "--ignore-submodules=all", "-z", head)
	if err != nil {
		return nil, false, err
	}
	var rehash []string
	for _, p := range changed {
		delete(idx, p) // read from disk below, or gone
		rehash = append(rehash, p)
	}
	for _, p := range untracked {
		if !strings.HasSuffix(p, "/") { // "dir/" is a nested repository or an opaque directory: not ours
			rehash = append(rehash, p)
		}
	}
	files := make([]scopedFile, 0, len(idx)+len(rehash))
	tracked := map[string]string{}
	for p, e := range idx {
		files = append(files, scopedFile{path: p, mode: e.mode, id: e.id})
		tracked[p] = e.mode
	}
	for _, p := range changed {
		if e, ok := tracked[p]; ok {
			_ = e
		}
	}
	hashed, err := hashOnDisk(repo, rehash)
	if err != nil {
		return nil, false, err
	}
	files = append(files, hashed...)
	if err := headStill(repo, head); err != nil {
		return nil, false, err
	}
	return files, len(diff) > 0 || len(untracked) > 0, nil
}

// listPaths runs a git command that lists NUL-separated paths and returns
// those keep accepts.
func listPaths(repo string, keep func(string) bool, args ...string) ([]string, error) {
	out, err := gitRaw(repo, args...)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" && (strings.HasSuffix(p, "/") || keep(p)) {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// hashOnDisk identifies the files at paths (relative to repo) as they are on
// disk: symlinks by their target, regular files by their filtered content,
// anything else (and a file that is gone) not at all.
func hashOnDisk(repo string, paths []string) ([]scopedFile, error) {
	var regular []string
	modes := map[string]string{}
	var out []scopedFile
	execBits := execBitsReliable(repo)
	for _, p := range paths {
		if strings.ContainsAny(p, "\n") || strings.HasPrefix(p, `"`) {
			return nil, fmt.Errorf("%q: a path with a newline or a leading quote cannot be identified", p)
		}
		full := filepath.Join(repo, filepath.FromSlash(p))
		fi, err := os.Lstat(full)
		switch {
		case err != nil && !os.IsNotExist(err):
			return nil, err
		case err != nil:
			continue // deleted: not part of the tree
		case fi.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(full)
			if err != nil {
				return nil, err
			}
			id, err := hashBlob(repo, target)
			if err != nil {
				return nil, err
			}
			out = append(out, scopedFile{path: p, mode: modeSymlink, id: id})
		case fi.Mode().IsRegular():
			regular = append(regular, p)
			modes[p] = modeFile
			if execBits && fi.Mode()&0o111 != 0 {
				modes[p] = modeExec
			}
		}
	}
	ids, err := hashFiles(repo, regular)
	if err != nil {
		return nil, err
	}
	for i, p := range regular {
		out = append(out, scopedFile{path: p, mode: modes[p], id: ids[i]})
	}
	return out, nil
}

// execBitsReliable is core.filemode: false on file systems where every file
// looks executable (or none does), where the bit says nothing about the file.
func execBitsReliable(repo string) bool {
	out, err := git(repo, "config", "--type=bool", "core.filemode")
	return err != nil || out != "false"
}

// hashFiles returns git's blob id for each of paths, in order, in one process.
func hashFiles(repo string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	var out bytes.Buffer
	if err := gitWithInput(&out, strings.NewReader(strings.Join(paths, "\n")+"\n"), repo, "hash-object", "--stdin-paths"); err != nil {
		return nil, err
	}
	ids := strings.Fields(out.String())
	if len(ids) != len(paths) {
		return nil, fmt.Errorf("git hash-object: %d ids for %d files", len(ids), len(paths))
	}
	return ids, nil
}

// hashBlob returns git's blob id for content.
func hashBlob(repo, content string) (string, error) {
	var out bytes.Buffer
	if err := gitWithInput(&out, strings.NewReader(content), repo, "hash-object", "--stdin"); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

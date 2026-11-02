package gitutil

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/pathglob"
)

// scopedIDLen is how many hex characters of the digest a scoped tree id has:
// the length of a git tree id, so everything that shows a tree shows this too.
const scopedIDLen = 40

// scopedSchema versions the encoding of a scoped tree id; changing it changes
// every id, so old proofs simply never match.
const scopedSchema = 1

// ScopedTree identifies only the files of repo that match globs (see package
// pathglob), so a change anywhere else (docs, a changelog) leaves the id as it
// was. It is what a gate with `paths` is proven on.
//
// commit "" reads the work tree as it is now: tracked files with their
// current content (staged or not), and untracked files that are not ignored,
// so an edit, a new file or a deletion of a matching file changes the id, and
// undoing it restores it. A commit names a pinned commit (what a remote run
// executes). For the same files the two give the same id, and so does the work
// tree before and after a commit that records its edits: the id is a hash of
// (path, mode, content id) of the matching files and of the globs themselves,
// nothing about the machine, the checkout directory, times or git config.
//
// heavyq's bookkeeping files (see bookkeepingPaths) and NestedWorkTreeDirs
// never count, as in the whole-tree identity. An error means the files cannot
// be identified reliably (unmerged paths, a file that vanished while being
// read, HEAD that moved): the caller must treat the tree as unknown.
func ScopedTree(repo, commit string, globs []string) (TreeID, error) {
	m, err := pathglob.Compile(globs)
	if err != nil {
		return TreeID{}, err
	}
	if len(globs) == 0 {
		return TreeID{}, fmt.Errorf("no paths to scope the tree to")
	}
	skip := excluder(append(bookkeepingPaths(repo, commit), NestedWorkTreeDirs...))
	keep := func(p string) bool { return m.Match(p) && !skip(p) }
	var (
		files []scopedFile
		dirty bool
	)
	if commit != "" {
		files, err = committedFiles(repo, commit, keep)
	} else {
		files, dirty, err = workTreeFiles(repo, keep)
	}
	if err != nil {
		return TreeID{}, err
	}
	return TreeID{ID: scopedDigest(globs, files), Dirty: dirty}, nil
}

// scopedFile is one matching file: its path, git mode and content id.
type scopedFile struct{ path, mode, id string }

// scopedDigest hashes the globs and the files, in path order.
func scopedDigest(globs []string, files []scopedFile) string {
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	g := append([]string(nil), globs...)
	sort.Strings(g)
	h := sha256.New()
	fmt.Fprintf(h, "heavyq-scope-v%d\n%d\n", scopedSchema, len(g))
	for _, p := range g {
		fmt.Fprintf(h, "%s\x00", p)
	}
	for _, f := range files {
		fmt.Fprintf(h, "\n%s\x00%s\x00%s", f.path, f.mode, f.id)
	}
	return hex.EncodeToString(h.Sum(nil))[:scopedIDLen]
}

// excluder returns the test for "p is one of paths or below it".
func excluder(paths []string) func(string) bool {
	return func(p string) bool {
		for _, x := range paths {
			if p == x || strings.HasPrefix(p, x+"/") {
				return true
			}
		}
		return false
	}
}

// committedFiles lists the matching files of commit with git's own blob ids.
func committedFiles(repo, commit string, keep func(string) bool) ([]scopedFile, error) {
	out, err := gitRaw(repo, "ls-tree", "-r", "-z", "--full-tree", commit)
	if err != nil {
		return nil, err
	}
	var files []scopedFile
	for _, entry := range strings.Split(out, "\x00") {
		meta, path, ok := strings.Cut(entry, "\t")
		if !ok {
			continue
		}
		f := strings.Fields(meta) // mode, type, id
		if len(f) != 3 || f[1] != "blob" || !keep(path) {
			continue // a submodule, or not in scope
		}
		files = append(files, scopedFile{path: path, mode: f[0], id: f[2]})
	}
	return files, nil
}

// indexEntry is one tracked file as the index records it.
type indexEntry struct{ mode, id string }

// trackedIndex reads the matching, tracked files from the index. A path with
// conflict stages is an error: its content is not one thing.
func trackedIndex(repo string, keep func(string) bool) (map[string]indexEntry, error) {
	out, err := gitRaw(repo, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, err
	}
	idx := map[string]indexEntry{}
	for _, entry := range strings.Split(out, "\x00") {
		meta, path, ok := strings.Cut(entry, "\t")
		if !ok || !keep(path) {
			continue
		}
		f := strings.Fields(meta) // mode, id, stage
		switch {
		case len(f) != 3 || f[0] == gitlinkMode:
		case f[2] != "0":
			return nil, fmt.Errorf("%s has unmerged changes", path)
		default:
			idx[path] = indexEntry{mode: f[0], id: f[1]}
		}
	}
	return idx, nil
}

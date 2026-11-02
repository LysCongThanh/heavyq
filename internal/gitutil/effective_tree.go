package gitutil

import (
	"bytes"
	"crypto/sha1" //nolint:gosec // git's own object format for sha1 repositories, not a security use
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// effectiveTreeOf is the identity of commit's committed code: its root tree
// with the bookkeeping paths removed (see bookkeepingPaths).
//
// It is the commit's tree itself, found with one `git cat-file`, for a
// repository that has none of those paths (most of them). Otherwise only the
// levels on the way to a bookkeeping path are read and re-hashed, as git
// would hash the tree without them; every other subtree keeps its own id, so
// the cost is the number of entries on those few levels, never the number of
// files.
//
// The result has the shape of a git tree id and equals the id of the tree
// the commit would have without those files, so two commits that differ only
// in bookkeeping files get the same identity, and so does a commit and a work
// tree that differs from it only in them. It is a heavyq identity, not a
// promise that the object exists: nothing is written to the object database.
func effectiveTreeOf(repo, commit string, exclude []string) (string, error) {
	root, entries, err := readTree(repo, commit+"^{tree}")
	if err != nil {
		return "", err
	}
	id, _, err := newPathTrie(exclude).apply(repo, root, entries)
	return id, err
}

// pathTrie is a set of slash-separated paths, one node per path element.
type pathTrie struct {
	leaf bool // this path is excluded, with everything below it
	kids map[string]*pathTrie
}

func newPathTrie(paths []string) *pathTrie {
	root := &pathTrie{}
	for _, p := range paths {
		n := root
		for _, el := range strings.Split(p, "/") {
			if n.kids == nil {
				n.kids = map[string]*pathTrie{}
			}
			if n.kids[el] == nil {
				n.kids[el] = &pathTrie{}
			}
			n = n.kids[el]
		}
		n.leaf = true
	}
	return root
}

// treeEntry is one entry of a tree object; mode has no padding ("40000").
type treeEntry struct {
	mode, kind, hash, name string
}

// apply returns the id of tree (a tree id, holding entries) without the
// excluded paths below n, and whether nothing is left in it. It returns tree
// itself, untouched, as long as no excluded path is found on the way.
func (n *pathTrie) apply(repo, tree string, entries []treeEntry) (id string, empty bool, err error) {
	kept, changed := make([]treeEntry, 0, len(entries)), false
	for _, e := range entries {
		kid := n.kids[e.name]
		switch {
		case kid == nil:
		case kid.leaf:
			changed = true
			continue
		case e.kind == "tree":
			_, subEntries, err := readTree(repo, e.hash)
			if err != nil {
				return "", false, err
			}
			sub, subEmpty, err := kid.apply(repo, e.hash, subEntries)
			if err != nil {
				return "", false, err
			}
			if sub != e.hash {
				changed = true
				if subEmpty {
					continue // git has no empty trees: the directory is gone
				}
				e.hash = sub
			}
		}
		kept = append(kept, e)
	}
	if !changed {
		return tree, len(entries) == 0, nil
	}
	id, err = hashTree(tree, kept)
	return id, len(kept) == 0, err
}

// readTree reads one level of treeish with `git cat-file --batch`, which
// gives the tree's id and its entries in a single process (rev-parse plus
// ls-tree would take two): "<id> tree <size>\n", then the tree object with
// its entries as "<mode> <name>\0<raw id>" in git's order, then "\n".
func readTree(repo, treeish string) (id string, entries []treeEntry, err error) {
	var out bytes.Buffer
	if err := gitWithInput(&out, strings.NewReader(treeish+"\n"), repo, "cat-file", "--batch"); err != nil {
		return "", nil, err
	}
	hdr, rest, ok := bytes.Cut(out.Bytes(), []byte("\n"))
	f := strings.Fields(string(hdr))
	if !ok || len(f) != 3 || f[1] != "tree" || !shaRe.MatchString(f[0]) {
		return "", nil, fmt.Errorf("%s: no tree for %q", repo, treeish)
	}
	size, err := strconv.Atoi(f[2])
	if err != nil || size < 0 || size > len(rest) {
		return "", nil, fmt.Errorf("git cat-file %s: bad tree size %q", treeish, f[2])
	}
	rawLen, body := len(f[0])/2, rest[:size]
	for len(body) > 0 {
		meta, tail, found := bytes.Cut(body, []byte{0})
		mode, name, spaced := strings.Cut(string(meta), " ")
		if !found || !spaced || len(tail) < rawLen {
			return "", nil, fmt.Errorf("git cat-file %s: malformed tree entry", treeish)
		}
		kind := "blob"
		switch mode {
		case "40000":
			kind = "tree"
		case "160000":
			kind = "commit"
		}
		entries = append(entries, treeEntry{mode: mode, kind: kind, hash: hex.EncodeToString(tail[:rawLen]), name: name})
		body = tail[rawLen:]
	}
	return f[0], entries, nil
}

// hashTree computes the git id of a tree object holding entries, with the
// hash function of the repository (told by the length of like, any id from
// it). The entries keep their order: removing some leaves git's order intact.
func hashTree(like string, entries []treeEntry) (string, error) {
	var body []byte
	for _, e := range entries {
		raw, err := hex.DecodeString(e.hash)
		if err != nil {
			return "", fmt.Errorf("tree entry %q: bad id %q", e.name, e.hash)
		}
		body = append(body, e.mode+" "+e.name+"\x00"...)
		body = append(body, raw...)
	}
	data := append([]byte("tree "+strconv.Itoa(len(body))+"\x00"), body...)
	switch len(like) {
	case 40:
		sum := sha1.Sum(data) //nolint:gosec // see the import
		return hex.EncodeToString(sum[:]), nil
	case 64:
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:]), nil
	}
	return "", fmt.Errorf("tree id %q: unknown object format", like)
}

package gitutil

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// maxHashedFileBytes bounds the cost of identifying a tree: an untracked file
// larger than this (a model, a database dump) is identified by size and
// modification time instead of by content. That can only make an id change
// when nothing was edited, never the reverse, so a stale proof is not possible.
const maxHashedFileBytes = 64 << 20

// untrackedDigest hashes the names and contents of the untracked files that
// are not ignored, in the order git lists them. repo must be the work tree
// root: ls-files reports paths relative to where it runs.
func untrackedDigest(repo string) (string, error) {
	list, err := gitRaw(repo, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return "", err
	}
	outer, inner, buf := sha256.New(), sha256.New(), make([]byte, 64<<10)
	for _, name := range strings.Split(list, "\x00") {
		if name == "" {
			continue
		}
		kind, sum := digestEntry(inner, buf, filepath.Join(repo, filepath.FromSlash(name)))
		// Fixed-width digests keep the stream unambiguous whatever the content.
		fmt.Fprintf(outer, "%s\x00%c", name, kind)
		outer.Write(sum)
	}
	return hex.EncodeToString(outer.Sum(nil)), nil
}

// digestEntry returns a one-letter kind and a 32-byte digest of one path:
// F regular file (content), B big file (size and mtime), L symlink (its
// target, not followed), U unreadable (size), G gone (listed, then removed)
// or O anything else (a nested repository directory, a socket, ...).
func digestEntry(h hash.Hash, buf []byte, path string) (byte, []byte) {
	h.Reset()
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 'G', h.Sum(nil)
	}
	if err != nil {
		return 'U', h.Sum(nil)
	}
	switch {
	case fi.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return 'U', h.Sum(nil)
		}
		h.Write([]byte(target))
		return 'L', h.Sum(nil)
	case !fi.Mode().IsRegular():
		return 'O', h.Sum(nil)
	case fi.Size() > maxHashedFileBytes:
		fmt.Fprintf(h, "%d:%d", fi.Size(), fi.ModTime().UnixNano())
		return 'B', h.Sum(nil)
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 'G', h.Sum(nil)
	}
	if err != nil {
		fmt.Fprintf(h, "%d", fi.Size())
		return 'U', h.Sum(nil)
	}
	defer f.Close()
	if _, err := io.CopyBuffer(h, f, buf); err != nil {
		h.Reset()
		fmt.Fprintf(h, "%d", fi.Size())
		return 'U', h.Sum(nil)
	}
	return 'F', h.Sum(nil)
}

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

// The limits are variables so tests can lower them.
var (
	// maxHashedFileBytes bounds the cost of identifying one file: an untracked
	// file larger than this (a model, a database dump) is identified by size,
	// modification time, inode and change time instead of by content.
	//
	// That is weaker than content, so the comment must not overpromise: a
	// rewrite that keeps all four the same needs the kernel's change time to
	// repeat, which user-space tools (cp -p, rsync -t, tar x, touch -r) cannot
	// arrange, but a coarse filesystem clock could in theory repeat for two
	// writes in the same tick. Where the stamp is unavailable the tree is
	// unknown rather than guessed.
	maxHashedFileBytes int64 = 64 << 20

	// maxUntrackedBytes bounds the content read per identification (it is done
	// before and after every local run): past it the tree is unknown, so a
	// large untracked directory costs one failed identification, not a re-read
	// of everything on every run.
	maxUntrackedBytes int64 = 256 << 20
)

// Entry kinds, one letter in the digest stream.
const (
	kindFile      = 'F' // regular file, content hashed
	kindBig       = 'B' // regular file over maxHashedFileBytes: size, mtime, inode, ctime
	kindLink      = 'L' // symlink: its target, not followed
	kindUnread    = 'U' // unreadable: size only
	kindGone      = 'G' // listed, then removed
	kindOther     = 'O' // a socket, fifo, ...
	kindUnbounded = 'X' // over the byte budget, or no stamp for a big file: unknown
)

// untrackedResult is what untrackedDigest learned.
type untrackedResult struct {
	digest  string
	files   int  // entries listed
	unknown bool // cannot be identified reliably (see TreeID)
}

// untrackedDigest hashes the names and contents of the untracked files that
// are not ignored, in the order git lists them. repo must be the work tree
// root: ls-files reports paths relative to where it runs.
//
// A nested repository (an untracked directory holding its own .git) is listed
// by git as "dir/" and its contents are not visible from here, so it makes the
// result unknown.
func untrackedDigest(repo string) (untrackedResult, error) {
	list, err := gitRaw(repo, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return untrackedResult{}, err
	}
	outer, inner, buf := sha256.New(), sha256.New(), make([]byte, 64<<10)
	budget := maxUntrackedBytes
	res := untrackedResult{}
	for _, name := range strings.Split(list, "\x00") {
		if name == "" {
			continue
		}
		res.files++
		if strings.HasSuffix(name, "/") {
			res.unknown = true
			return res, nil
		}
		kind, sum := digestEntry(inner, buf, &budget, filepath.Join(repo, filepath.FromSlash(name)))
		if kind == kindUnbounded {
			res.unknown = true
			return res, nil
		}
		// Fixed-width digests keep the stream unambiguous whatever the content.
		fmt.Fprintf(outer, "%s\x00%c", name, kind)
		outer.Write(sum)
	}
	res.digest = hex.EncodeToString(outer.Sum(nil))
	return res, nil
}

// digestEntry returns a one-letter kind (see the kind constants) and a 32-byte
// digest of one path. Reading a file spends its size from budget.
func digestEntry(h hash.Hash, buf []byte, budget *int64, path string) (byte, []byte) {
	h.Reset()
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return kindGone, h.Sum(nil)
	}
	if err != nil {
		return kindUnread, h.Sum(nil)
	}
	switch {
	case fi.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return kindUnread, h.Sum(nil)
		}
		h.Write([]byte(target))
		return kindLink, h.Sum(nil)
	case !fi.Mode().IsRegular():
		return kindOther, h.Sum(nil)
	case fi.Size() > maxHashedFileBytes:
		ino, ctime, ok := fileStamp(fi)
		if !ok {
			return kindUnbounded, nil
		}
		fmt.Fprintf(h, "%d:%d:%d:%d", fi.Size(), fi.ModTime().UnixNano(), ino, ctime)
		return kindBig, h.Sum(nil)
	}
	if fi.Size() > *budget {
		return kindUnbounded, nil
	}
	*budget -= fi.Size()
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return kindGone, h.Sum(nil)
	}
	if err != nil {
		fmt.Fprintf(h, "%d", fi.Size())
		return kindUnread, h.Sum(nil)
	}
	defer f.Close()
	if _, err := io.CopyBuffer(h, f, buf); err != nil {
		h.Reset()
		fmt.Fprintf(h, "%d", fi.Size())
		return kindUnread, h.Sum(nil)
	}
	return kindFile, h.Sum(nil)
}

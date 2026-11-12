package repoconfig

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"
)

// CheckAnchorFile is the one rule for `[anchor] file`, shared by validation
// here and by gitutil, which leaves that path out of every tree identity:
// whatever it names stops counting as code for proofs and the cache. So it
// must name a single markdown file inside the repository: a local path, with
// no control characters, not ending in "/", whose name ends in ".md". A
// directory or a code file there would take code out of what a proof covers.
//
// It checks the text only; whether the path is a directory on disk or in a
// commit is the caller's to check (see AnchorFileIsDirectory).
func CheckAnchorFile(p string) error {
	switch {
	case !filepath.IsLocal(p):
		return fmt.Errorf("file %q must be a relative path inside the repository", p)
	case strings.IndexFunc(p, unicode.IsControl) >= 0:
		return fmt.Errorf("file %q contains a control character", p)
	case strings.HasSuffix(filepath.ToSlash(p), "/"):
		return fmt.Errorf("file %q must name a .md file, not a directory", p)
	case !strings.EqualFold(path.Ext(path.Clean(filepath.ToSlash(p))), ".md"):
		return fmt.Errorf("file %q must name a .md file (it is left out of the tree id, so it cannot be code or a directory)", p)
	}
	return nil
}

// AnchorFileIsDirectory reports whether the anchor file p (already accepted
// by CheckAnchorFile) is a directory in the work tree at root. A missing path
// is not one: nothing below it could be hidden. A path that cannot be checked
// counts as one, so the caller fails closed.
func AnchorFileIsDirectory(root, p string) bool {
	fi, err := os.Lstat(filepath.Join(root, filepath.FromSlash(p)))
	if err != nil {
		return !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR)
	}
	return fi.IsDir()
}

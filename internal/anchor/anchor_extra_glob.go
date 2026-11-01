package anchor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// expandGlob returns the repo-relative slash paths of the regular files under
// root that match pattern, sorted. Beyond path.Match it understands "**" as a
// whole segment ("docs/**/*.md" matches docs/a.md and docs/x/y/b.md).
//
// Symlinks are never followed out of the repository: a match whose real path
// is outside root, or that is not a regular file, is dropped. ".git" is not
// searched.
func expandGlob(root, pattern string) ([]string, error) {
	pattern = path.Clean(filepath.ToSlash(pattern))
	segs := strings.Split(pattern, "/")
	for _, s := range segs {
		if _, err := path.Match(s, ""); err != nil {
			return nil, fmt.Errorf("glob %q: %w", pattern, err)
		}
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	fsys := os.DirFS(root)
	var found []string
	if !containsSeg(segs, "**") {
		found, err = fs.Glob(fsys, pattern)
	} else {
		found, err = walkGlob(fsys, segs)
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, rel := range found {
		if regularInside(realRoot, filepath.Join(root, filepath.FromSlash(rel))) {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out, nil
}

func containsSeg(segs []string, want string) bool {
	for _, s := range segs {
		if s == want {
			return true
		}
	}
	return false
}

// walkGlob walks from the longest meta-free directory prefix of the pattern.
func walkGlob(fsys fs.FS, segs []string) ([]string, error) {
	start := "."
	for i, s := range segs {
		if strings.ContainsAny(s, `*?[\`) || i == len(segs)-1 {
			start = path.Join(append([]string{"."}, segs[:i]...)...)
			break
		}
	}
	var out []string
	err := fs.WalkDir(fsys, start, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll // prefix directory is missing: no matches
			}
			return nil // unreadable subtree: skip it
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() && matchSegs(segs, strings.Split(p, "/")) {
			out = append(out, p)
		}
		return nil
	})
	return out, err
}

// matchSegs matches name segments against pattern segments; "**" matches any
// number of segments, including none.
func matchSegs(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for len(pat) > 1 && pat[1] == "**" {
				pat = pat[1:]
			}
			if len(pat) == 1 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchSegs(pat[1:], name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, err := path.Match(pat[0], name[0]); err != nil || !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}

// regularInside reports whether p is a regular file whose real location is
// inside realRoot (an already symlink-resolved path).
func regularInside(realRoot, p string) bool {
	if fi, err := os.Lstat(p); err != nil || !fi.Mode().IsRegular() {
		return false
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(realRoot, real)
	return err == nil && filepath.IsLocal(rel)
}

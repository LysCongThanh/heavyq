package anchor

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/LysCongThanh/heavyq/internal/repoconfig"
)

// Load reads the anchor file (cfg.File, default DefaultFile) and the extra
// sources (cfg.Extra globs), all relative to repoRoot, and merges them:
// anchor.md items first, then extras in glob order, files sorted. Missing
// files are fine; the result then just has fewer items. Files that cannot be
// read or are over 1 MiB are skipped with a Warning. At most MaxItems items
// are returned.
//
// Errors are reserved for unusable configuration (a path that escapes the
// repository, a malformed glob) and for an unreadable anchor file.
func Load(repoRoot string, cfg repoconfig.Anchor) (*File, error) {
	rel := cfg.File
	if rel == "" {
		rel = DefaultFile
	}
	if !filepath.IsLocal(rel) {
		return nil, fmt.Errorf("anchor file %q must be a relative path inside the repository", rel)
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	f := &File{Path: rel, Items: []Item{}, Warnings: []Warning{}}
	used := map[string]bool{}

	data, err := readLimited(filepath.Join(repoRoot, filepath.FromSlash(rel)))
	switch {
	case err == nil:
		p := Parse(rel, data)
		f.Goal, f.GoalLine, f.Items, f.Warnings = p.Goal, p.GoalLine, p.Items, p.Warnings
		for _, it := range p.Items {
			used[it.ID] = true
		}
	case errors.Is(err, fs.ErrNotExist):
	case errors.Is(err, ErrTooLarge), errors.Is(err, ErrNotRegular):
		f.warn(rel, 0, err.Error()+"; ignored")
	default:
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}

	seen := map[string]bool{rel: true}
	for _, glob := range cfg.Extra {
		if !filepath.IsLocal(glob) {
			return nil, fmt.Errorf("extra glob %q must be relative and inside the repository", glob)
		}
		matches, err := expandGlob(repoRoot, glob)
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			if seen[m] {
				continue
			}
			seen[m] = true
			if !f.addExtra(repoRoot, m, used) {
				return f, nil
			}
		}
	}
	return f, nil
}

func (f *File) warn(src string, line int, msg string) {
	f.Warnings = append(f.Warnings, Warning{Source: src, Line: line, Msg: msg})
}

// addExtra appends the items of one extra file. It returns false once the
// item cap is reached.
func (f *File) addExtra(repoRoot, rel string, used map[string]bool) bool {
	data, err := readLimited(filepath.Join(repoRoot, filepath.FromSlash(rel)))
	if err != nil {
		f.warn(rel, 0, err.Error()+"; ignored")
		return true
	}
	items, warns := ParseExtra(rel, data)
	f.Warnings = append(f.Warnings, warns...)
	for _, it := range items {
		if len(f.Items) >= MaxItems {
			f.warn(rel, it.Line, "more than 2000 items in total; the rest are ignored")
			return false
		}
		id, dup := uniqueID(used, it.ID)
		if dup {
			f.warn(rel, it.Line, "id collides with another source; renamed to "+quote(id))
		}
		it.ID = id
		f.Items = append(f.Items, it)
	}
	return true
}

// readLimited reads a regular file of at most MaxFileBytes. It stats before
// opening so a FIFO cannot block the caller.
func readLimited(p string) ([]byte, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, ErrNotRegular
	}
	fh, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	data, err := io.ReadAll(io.LimitReader(fh, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileBytes {
		return nil, ErrTooLarge
	}
	return data, nil
}

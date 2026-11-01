package anchor

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// afterReadHook lets tests edit the file between the first read and the
// re-read. It is nil in production.
var afterReadHook func()

// SetItemChecked flips the checkbox of an item returned by Load. Items from
// extra sources are refused with ErrReadOnly: heavyq never edits plan files.
func SetItemChecked(repoRoot string, it Item, checked bool) error {
	if it.ReadOnly {
		return fmt.Errorf("%w: %s", ErrReadOnly, it.ID)
	}
	return SetChecked(filepath.Join(repoRoot, filepath.FromSlash(it.Source)), it.ID, checked)
}

// SetChecked sets the checkbox of item id in the anchor file at path. It
// changes exactly one byte (the character between the brackets) and keeps
// every other byte, so CRLF, BOM, tabs and trailing spaces survive. Setting
// the state an item already has writes nothing.
//
// It returns ErrNotFound when no item has that id (ids of extra sources
// never match: they contain "/"), ErrNotRegular for symlinks and other
// non-regular files, and ErrConcurrent when the item's line changed between
// the first read and the write. The file is replaced atomically (temp file in
// the same directory, then rename) with its permission bits kept.
//
// Writers are not serialised: two processes that flip different items at the
// same instant can lose one update in the rename window. Callers that need
// that guarantee hold their own lock; the re-read narrows the window to
// microseconds.
func SetChecked(path, id string, checked bool) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%w: %s", ErrNotRegular, path)
	}
	data, err := readLimited(path)
	if err != nil {
		return err
	}
	it, ok := findItem(Parse(path, data), id)
	if !ok {
		return fmt.Errorf("%w: %q in %s", ErrNotFound, id, path)
	}
	if it.Checked == checked {
		return nil
	}
	out, err := flipBox(data, it.Line, checked)
	if err != nil {
		return err
	}

	// Re-read right before writing; an edit that did not touch this item's
	// line is merged by flipping the box in the fresh bytes instead.
	if afterReadHook != nil {
		afterReadHook()
	}
	fresh, err := readLimited(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(fresh, data) {
		cur, ok := findItem(Parse(path, fresh), id)
		if !ok || cur.Line != it.Line || cur.Checked != it.Checked || !sameLine(data, fresh, it.Line) {
			return fmt.Errorf("%w: %s", ErrConcurrent, id)
		}
		if out, err = flipBox(fresh, it.Line, checked); err != nil {
			return err
		}
	}
	return writeAtomic(path, out, fi.Mode().Perm())
}

func findItem(f File, id string) (Item, bool) {
	for _, it := range f.Items {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

// sameLine reports whether 1-based line n has the same bytes in a and b.
func sameLine(a, b []byte, n int) bool {
	la, lb := splitLines(a), splitLines(b)
	if n > len(la) || n > len(lb) {
		return false
	}
	return bytes.Equal(a[la[n-1].start:la[n-1].end], b[lb[n-1].start:lb[n-1].end])
}

// flipBox returns a copy of data with the checkbox of 1-based line n set.
func flipBox(data []byte, n int, checked bool) ([]byte, error) {
	spans := splitLines(data)
	if n < 1 || n > len(spans) {
		return nil, fmt.Errorf("%w: line %d vanished", ErrConcurrent, n)
	}
	line, skip := lineText(data, spans[n-1], n == 1)
	col, _, _, ok := matchItemLine(line)
	if !ok {
		return nil, fmt.Errorf("%w: line %d is no longer a checkbox", ErrConcurrent, n)
	}
	out := bytes.Clone(data)
	out[spans[n-1].start+skip+col] = ' '
	if checked {
		out[spans[n-1].start+skip+col] = 'x'
	}
	return out, nil
}

// writeAtomic replaces path with data via a temp file and rename.
func writeAtomic(path string, data []byte, mode os.FileMode) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Chmod(mode); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

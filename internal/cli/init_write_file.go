package cli

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeNewFile creates path with text, exclusively: an existing file, or a
// symlink even if dangling, is never touched and the error is fs.ErrExist.
func writeNewFile(path, text string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.WriteString(text)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(path) // do not leave half a file that blocks the next run
		return fmt.Errorf("write %s: %w", path, werr)
	}
	return nil
}

// replaceFile writes data to path through a temporary file in the same
// directory and a rename, so a reader sees the old or the new content, never
// half of it. A symlink at path is replaced by a regular file, not written
// through.
func replaceFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), mode)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

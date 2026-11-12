package hook

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// WriteSettings backs up an existing file to path.bak, then replaces path
// atomically, keeping its permission bits. A path that is a symlink (a
// dotfiles checkout) stays one: the link's target is replaced instead, and a
// link that does not resolve is refused rather than overwritten.
func WriteSettings(path string, data []byte) error {
	target, err := settingsTarget(path)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if old, err := os.ReadFile(target); err == nil {
		if fi, err := os.Stat(target); err == nil {
			mode = fi.Mode().Perm()
		}
		if err := os.WriteFile(path+".bak", old, mode); err != nil {
			return fmt.Errorf("backup: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".heavyq-settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}

// settingsTarget is the file a write to path must replace: path itself, or
// the file its symlink chain ends at.
func settingsTarget(path string) (string, error) {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return path, nil
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("%s is a symlink that does not resolve, refusing to replace it: %w", path, err)
	}
	return real, nil
}

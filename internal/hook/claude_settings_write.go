package hook

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// WriteSettings backs up an existing file to path.bak, then replaces path
// atomically, keeping its permission bits.
func WriteSettings(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if old, err := os.ReadFile(path); err == nil {
		if fi, err := os.Stat(path); err == nil {
			mode = fi.Mode().Perm()
		}
		if err := os.WriteFile(path+".bak", old, mode); err != nil {
			return fmt.Errorf("backup: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".heavyq-settings-*")
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
	return os.Rename(tmp.Name(), path)
}

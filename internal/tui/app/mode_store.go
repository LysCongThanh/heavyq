package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/tui/render"
)

// StatePath is where the last display mode is remembered:
// $XDG_STATE_HOME/heavyq/top-mode, else ~/.local/state/heavyq/top-mode.
// It returns "" when there is no home directory.
func StatePath() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" && filepath.IsAbs(d) {
		return filepath.Join(d, "heavyq", "top-mode")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "state", "heavyq", "top-mode")
}

// LoadMode reads the remembered mode; ok is false when there is none or the
// file holds anything but a mode name (a stale or hand-edited file is ignored).
func LoadMode(path string) (render.Mode, bool) {
	if path == "" {
		return render.ModeFull, false
	}
	b, err := os.ReadFile(path)
	if err != nil || len(b) > 32 {
		return render.ModeFull, false
	}
	return render.ParseMode(strings.TrimSpace(string(b)))
}

// SaveMode remembers mode. Failure is returned for the caller to ignore: not
// being able to remember must never disturb the screen.
func SaveMode(path string, mode render.Mode) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(mode.String()+"\n"), 0o600)
}

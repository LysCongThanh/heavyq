package steer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// EnvVar overrides the stored mode for one process tree. HEAVYQ_STEER=off is
// the kill switch.
const EnvVar = "HEAVYQ_STEER"

// NativeCapEnv is Claude Code's own cap on consecutive Stop-hook blocks
// (default 8). `0` removes it, which destroy mode needs.
const NativeCapEnv = "CLAUDE_CODE_STOP_HOOK_BLOCK_CAP"

// Mode is how hard heavyq steers an agent.
type Mode string

// The modes. The default, when nothing is stored, is Off: the gate is opt-in.
const (
	// Off: no blocking, no continuation. The brief is unaffected.
	Off Mode = "off"
	// Guard: Stop blocks while the lane holds claimed items that are not
	// proven, at most GuardBlockCap times per session, and never twice in a
	// row on an unchanged situation (stop_hook_active).
	Guard Mode = "guard"
	// Destroy: no cap, and SubagentStop, TaskCompleted and TeammateIdle are
	// gated too. The exits are proof, `item block`, `item release` and the
	// kill switch.
	Destroy Mode = "destroy"
)

// ParseMode accepts exactly the three words.
func ParseMode(s string) (Mode, error) {
	switch m := Mode(strings.TrimSpace(s)); m {
	case Off, Guard, Destroy:
		return m, nil
	}
	return "", fmt.Errorf("steer mode %q: want off, guard or destroy", s)
}

// ModeFile is where the stored mode lives, shared by every worktree.
func ModeFile(commonDir string) string { return filepath.Join(commonDir, "heavyq", "steer") }

// ReadStored returns the mode stored for the repository with this git common
// dir. A missing, unreadable, oversized or unknown file is Off: steering is
// opt-in, and a damaged file must never start blocking an agent.
func ReadStored(commonDir string) Mode {
	f, err := os.Open(ModeFile(commonDir))
	if err != nil {
		return Off
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 64))
	if err != nil {
		return Off
	}
	m, err := ParseMode(string(b))
	if err != nil {
		return Off
	}
	return m
}

// WriteStored stores m for the repository, atomically.
func WriteStored(commonDir string, m Mode) error {
	if _, err := ParseMode(string(m)); err != nil {
		return err
	}
	path := ModeFile(commonDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(string(m)+"\n"))
}

// Effective is the mode in force: the environment override when it is set and
// valid, else the stored mode. An override that is set but not a mode counts
// as off (a kill switch must never fail to kill), and src says so.
func Effective(env string, stored Mode) (m Mode, src string) {
	if strings.TrimSpace(env) == "" {
		return stored, "stored"
	}
	m, err := ParseMode(env)
	if err != nil {
		return Off, EnvVar + " is not off, guard or destroy: treated as off"
	}
	return m, EnvVar
}

// writeFileAtomic replaces path with data (mode 0600) via a temp file in the
// same directory, so a reader never sees half a file.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*") // 0600
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), path)
	}
	if werr != nil {
		os.Remove(tmp.Name())
	}
	return werr
}

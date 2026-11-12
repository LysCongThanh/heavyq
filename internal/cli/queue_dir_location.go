package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// Every heavyq of a user must find the same queue directory: the tickets, the
// slot locks and the pools all live there, so two directories are two queues
// that each admit their own heavy jobs. The environment is not a reliable
// guide: cron, `env -i` and services start without XDG_RUNTIME_DIR (Linux) or
// TMPDIR (macOS), which a login session has. So the per-user directories are
// found without it when it is missing.

// queueDirFrom is $HEAVYQ_DIR, else $XDG_RUNTIME_DIR/heavyq, else
// <runUser>/heavyq when that per-user runtime directory exists and is this
// user's own (the directory a login session's XDG_RUNTIME_DIR names), else
// <tmp>/heavyq-<uid> with tmp the per-user temporary directory.
func queueDirFrom(getenv func(string) string, runUser, tmp string) string {
	if d := getenv("HEAVYQ_DIR"); d != "" {
		return d
	}
	if d := getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "heavyq")
	}
	if ownPrivateDir(runUser) {
		return filepath.Join(runUser, "heavyq")
	}
	return filepath.Join(tmp, "heavyq-"+strconv.Itoa(os.Getuid()))
}

// ownPrivateDir reports whether dir is a directory of this user that nobody
// else can write to or list.
func ownPrivateDir(dir string) bool {
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() || fi.Mode().Perm()&0o077 != 0 {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}

// userRuntimeDir is the per-user runtime directory systemd-logind creates
// (/run/user/<uid>); it does not exist on macOS.
func userRuntimeDir() string { return filepath.Join("/run/user", strconv.Itoa(os.Getuid())) }

// userTempDir is the per-user temporary directory. On macOS that is $TMPDIR
// (/var/folders/.../T/), which a process started without it (cron, launchd,
// `env -i`) learns from getconf instead of falling back to the shared /tmp.
func userTempDir() string {
	if runtime.GOOS == "darwin" && os.Getenv("TMPDIR") == "" {
		if out, err := exec.Command("/usr/bin/getconf", "DARWIN_USER_TEMP_DIR").Output(); err == nil {
			if d := strings.TrimSpace(string(out)); filepath.IsAbs(d) {
				return d
			}
		}
	}
	return os.TempDir()
}

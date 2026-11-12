//go:build linux

package slots

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// heldByAnyone reads /proc/locks, the kernel's list of every held lock:
//
//	3: FLOCK  ADVISORY  WRITE 1234 fd:01:5678 0 EOF
//	3: -> FLOCK  ADVISORY  WRITE 1240 fd:01:5678 0 EOF   (a waiter, not a holder)
//
// The device is the file system's (sb->s_dev), which is stat's st_dev on
// most file systems but not on btrfs subvolumes; the file's mount, from
// fdinfo and mountinfo, gives the same number /proc/locks prints. A file
// with no lock line at all is free; a FLOCK line on it means busy; any other
// lock on it (POSIX, OFD, NFS's emulated flock) is left to the probe.
//
// /proc/locks leaves out every lock whose owner is not visible in the pid
// namespace of the /proc mount, so inside a container or sandbox that
// shares the queue dir a slot held from outside has no line. A missing line
// therefore means free only in the initial pid namespace; anywhere else the
// answer is "unknown" and Busy probes. A FLOCK line is a holder anywhere.
func heldByAnyone(path string) (busy, known bool) {
	locks, err := os.ReadFile("/proc/locks")
	if err != nil {
		return false, false
	}
	f, err := os.Open(path)
	if err != nil {
		return false, false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return false, false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return false, false
	}
	hits := locksOnInode(locks, st.Ino)
	if len(hits) == 0 {
		return false, seesAllLocks()
	}
	devs := map[string]bool{devKey(uint64(st.Dev)): true}
	if d, ok := mountDev(f); ok {
		devs[d] = true
	}
	other := false
	for _, h := range hits {
		if !devs[h.dev] {
			continue // the same inode number on another file system
		}
		if h.flock {
			return true, true
		}
		other = true
	}
	return false, !other && seesAllLocks()
}

// initPidNS is the inode of the initial pid namespace (PROC_PID_INIT_INO),
// fixed by the kernel since namespaces got inode numbers.
const initPidNS = "pid:[4026531836]"

// seesAllLocks reports whether /proc/locks lists every holder: this process
// is in the initial pid namespace and /proc is that namespace's. /proc/self
// does not resolve through a /proc mounted for a namespace this process is
// not in, so a readable link in the initial namespace settles both.
func seesAllLocks() bool {
	ns, err := os.Readlink("/proc/self/ns/pid")
	return err == nil && ns == initPidNS
}

type lockHit struct {
	dev   string // "maj:min" as /proc/locks prints it
	flock bool
}

// locksOnInode returns the held (not waiting) locks on inode ino.
func locksOnInode(locks []byte, ino uint64) []lockHit {
	suffix := ":" + strconv.FormatUint(ino, 10)
	var hits []lockHit
	sc := bufio.NewScanner(bytes.NewReader(locks))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 6 || f[1] == "->" {
			continue
		}
		if id := f[5]; strings.HasSuffix(id, suffix) {
			hits = append(hits, lockHit{dev: strings.TrimSuffix(id, suffix), flock: f[1] == "FLOCK"})
		}
	}
	return hits
}

// devKey formats a stat device number the way /proc/locks prints one.
func devKey(dev uint64) string {
	major := (dev>>8)&0xfff | (dev>>32)&^uint64(0xfff)
	minor := dev&0xff | (dev>>12)&^uint64(0xff)
	return fmt.Sprintf("%02x:%02x", major, minor)
}

// mountDev returns the device of the mount f lives on, in devKey form.
func mountDev(f *os.File) (string, bool) {
	info, err := os.ReadFile(fmt.Sprintf("/proc/self/fdinfo/%d", f.Fd()))
	if err != nil {
		return "", false
	}
	var mnt string
	for _, line := range strings.Split(string(info), "\n") {
		if v, ok := strings.CutPrefix(line, "mnt_id:"); ok {
			mnt = strings.TrimSpace(v)
		}
	}
	mounts, err := os.ReadFile("/proc/self/mountinfo")
	if mnt == "" || err != nil {
		return "", false
	}
	// 36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw
	for _, line := range strings.Split(string(mounts), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[0] != mnt {
			continue
		}
		hi, lo, ok := strings.Cut(f[2], ":")
		a, err1 := strconv.ParseUint(hi, 10, 32)
		b, err2 := strconv.ParseUint(lo, 10, 32)
		if !ok || err1 != nil || err2 != nil {
			return "", false
		}
		return fmt.Sprintf("%02x:%02x", a, b), true
	}
	return "", false
}

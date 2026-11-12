package metrics

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// commandTimeout bounds each sysctl / vm_stat run; both answer in
// milliseconds, so a hang means something is wrong and the gate falls back
// to slots only.
const commandTimeout = 5 * time.Second

// Host measures the machine heavyq runs on: macOS through sysctl and
// vm_stat (no cgo), everything else from procRoot like Local. A failing
// command yields Known=false, so the gate degrades to slots only exactly as
// it does on a platform that cannot be measured. The e2e tests may pin the
// reading (see test_override.go).
func Host(procRoot string) Metrics {
	var m Metrics
	if runtime.GOOS == "darwin" {
		m = localMacOS()
	} else {
		m = Local(procRoot)
	}
	if fake, ok := fakeFromEnv(m); ok {
		return fake
	}
	return m
}

// Where macOS installs the sampler's tools. They are called by absolute path
// because PATH may not hold them: cron runs jobs with PATH=/usr/bin:/bin, and
// sysctl lives in /usr/sbin, so a PATH lookup would silently turn the RAM/load
// gate off on a Mac that is perfectly measurable (the remote probe adds the
// same directories to PATH for the same reason).
var (
	darwinSysctlDirs = []string{"/usr/sbin", "/sbin"}
	darwinVMStatDirs = []string{"/usr/bin"}
)

// localMacOS samples the local Mac.
func localMacOS() Metrics {
	unknown := Metrics{Cores: runtime.NumCPU()}
	sysOut, err := run(toolPath("sysctl", darwinSysctlDirs), "-n", "hw.memsize", "hw.ncpu", "vm.loadavg")
	if err != nil {
		return unknown
	}
	vmOut, err := run(toolPath("vm_stat", darwinVMStatDirs))
	if err != nil {
		return unknown
	}
	m, err := ParseDarwin(sysOut, vmOut)
	if err != nil {
		return unknown
	}
	return m
}

// ParseDarwin turns the raw output of `sysctl -n hw.memsize hw.ncpu
// vm.loadavg` and of `vm_stat` into Metrics. The local sampler and the remote
// probe (a Mac reached over ssh) share it, so both use the same formulas.
func ParseDarwin(sysctlOut, vmStatOut string) (Metrics, error) {
	memBytes, ncpu, load1, err := parseSysctl(sysctlOut)
	if err != nil {
		return Metrics{}, err
	}
	avail, err := parseVMStat(vmStatOut)
	if err != nil {
		return Metrics{}, err
	}
	if avail > memBytes {
		avail = memBytes // the estimate can overlap itself; never exceed the RAM installed
	}
	return Metrics{
		FreeMB:  int(avail / bytesPerMB),
		TotalMB: int(memBytes / bytesPerMB),
		Load1:   load1,
		Cores:   ncpu,
		Known:   true,
	}, nil
}

// toolPath returns the first dirs/name that is an executable regular file, or
// name itself (a PATH lookup) when none is, e.g. on a system that moved it.
func toolPath(name string, dirs []string) string {
	for _, dir := range dirs {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0o111 != 0 {
			return p
		}
	}
	return name
}

// run executes a command with a fixed C locale and returns its stdout.
func run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	return string(out), err
}

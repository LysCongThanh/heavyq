package metrics

import (
	"context"
	"os"
	"os/exec"
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

// localMacOS samples the local Mac.
func localMacOS() Metrics {
	unknown := Metrics{Cores: runtime.NumCPU()}
	sysOut, err := run("sysctl", "-n", "hw.memsize", "hw.ncpu", "vm.loadavg")
	if err != nil {
		return unknown
	}
	vmOut, err := run("vm_stat")
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

// run executes a command with a fixed C locale and returns its stdout.
func run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	return string(out), err
}

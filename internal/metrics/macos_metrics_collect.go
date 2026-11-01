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
// it does on a platform that cannot be measured.
func Host(procRoot string) Metrics {
	if runtime.GOOS == "darwin" {
		return localMacOS()
	}
	return Local(procRoot)
}

// localMacOS samples the local Mac.
func localMacOS() Metrics {
	unknown := Metrics{Cores: runtime.NumCPU()}
	sysOut, err := run("sysctl", "-n", "hw.memsize", "hw.ncpu", "vm.loadavg")
	if err != nil {
		return unknown
	}
	memBytes, ncpu, load1, err := parseSysctl(sysOut)
	if err != nil {
		return unknown
	}
	vmOut, err := run("vm_stat")
	if err != nil {
		return unknown
	}
	avail, err := parseVMStat(vmOut)
	if err != nil {
		return unknown
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
	}
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

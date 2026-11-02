package pools

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// PortRange is an inclusive range of TCP ports.
type PortRange struct{ First, Last int }

// Contains reports whether port lies inside r.
func (r PortRange) Contains(port int) bool { return port >= r.First && port <= r.Last }

// fallbackEphemeral is the IANA dynamic range, used when the OS range cannot
// be read (an OS that is neither Linux nor macOS, or a failing sysctl).
var fallbackEphemeral = PortRange{First: 49152, Last: 65535}

var (
	ephOnce sync.Once
	ephVal  PortRange
)

// LocalEphemeralRange is the range the OS picks outgoing and port-0 ports
// from: a pool value inside it could be taken by any process at any moment.
// Linux reads /proc/sys/net/ipv4/ip_local_port_range, macOS asks
// net.inet.ip.portrange.first/last; anything else, or a failure, gives
// 49152-65535. It is read once.
func LocalEphemeralRange() PortRange {
	ephOnce.Do(func() { ephVal = readEphemeral() })
	return ephVal
}

func readEphemeral() PortRange {
	switch runtime.GOOS {
	case "linux":
		if b, err := os.ReadFile("/proc/sys/net/ipv4/ip_local_port_range"); err == nil {
			if r, err := ParseTwoPorts(string(b)); err == nil {
				return r
			}
		}
	case "darwin":
		out, err := exec.Command("sysctl", "-n", "net.inet.ip.portrange.first", "net.inet.ip.portrange.last").Output()
		if err == nil {
			if r, err := ParseTwoPorts(string(out)); err == nil {
				return r
			}
		}
	}
	return fallbackEphemeral
}

// ParseTwoPorts reads two whitespace-separated port numbers, the shape of
// both the Linux file ("32768\t60999") and the two lines of the macOS sysctl.
func ParseTwoPorts(s string) (PortRange, error) {
	f := strings.Fields(s)
	if len(f) != 2 {
		return PortRange{}, fmt.Errorf("want two port numbers, got %d fields", len(f))
	}
	lo, err1 := strconv.Atoi(f[0])
	hi, err2 := strconv.Atoi(f[1])
	if err1 != nil || err2 != nil || lo < 1 || hi > 65535 || lo > hi {
		return PortRange{}, fmt.Errorf("bad port range %q-%q", f[0], f[1])
	}
	return PortRange{First: lo, Last: hi}, nil
}

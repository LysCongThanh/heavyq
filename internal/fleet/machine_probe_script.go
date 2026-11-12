package fleet

import (
	"errors"
	"strconv"
	"time"

	"github.com/LysCongThanh/heavyq/internal/remote"
)

// fleetProbeScript measures one machine for `heavyq fleet`, read-only. It runs under bash through the
// machine's transport (`ssh host`, or `sh -c` for this machine and the tests). Args: <work_dir|-> <N>
// then <owner> <slot dir> pairs. It prints one fact per line after the marker "heavyq-fleet 1":
//
//	os, cores, load, mem_total_mb, mem_avail_mb    the machine (macOS: raw sysctl/vm_stat between darwin-begin/-end)
//	psi_cpu ... / psi_memory ...                    /proc/pressure lines, when the kernel has them
//	workdir <abs>                                   heavyq's work dir there, resolved
//	self <pid>                                      the script itself: it and its children are left out
//	p <pid> <ppid> <threads> <%cpu> <rss_kb> <comm> every process (ps %CPU)
//	clk <hz>, interval_ns <ns>, c <pid> <ticks>     Linux: CPU ticks each process used over a 1 s sample; this,
//	                                                not ps (CPU time over the process's whole life), is its %CPU
//	a<TAB><pid><TAB><args>                          the command line of the N busiest
//	w <pid>                                         a process whose cwd is in <work_dir>/wt (a heavyq remote job)
//	s <owner> <slot> <0|1> <holder pid|->           each <slot dir>/*.lock: held or not, and by whom
//
// A slot is read without being taken on Linux: /proc/locks says whether the file's inode is locked, and by
// which pid. macOS has no /proc/locks, so a non-blocking flock(2) is tried and released at once (as the
// scheduler's own probe does) and the holder is unknown.
const fleetProbeScript = `W="$1"; N="$2"; shift 2
case "$W" in -) W="" ;; /*) ;; *) W="$HOME/$W" ;; esac
echo "heavyq-fleet 1"; echo "self $$"
os=$(uname -s 2>/dev/null); echo "os ${os:-unknown}"
[ -n "$W" ] && echo "workdir $W"
if [ "$os" = Darwin ]; then
PATH="$PATH:/usr/sbin:/sbin:/usr/bin:/bin"
echo darwin-begin
LC_ALL=C sysctl -n hw.memsize hw.ncpu vm.loadavg 2>/dev/null
LC_ALL=C vm_stat 2>/dev/null
echo darwin-end
ps -Arco pid=,ppid=,pcpu=,rss=,comm= 2>/dev/null | awk '{printf "p %s %s 0 %s %s", $1, $2, $3, $4; for (i = 5; i <= NF; i++) printf " %s", $i; print ""}'
for p in $(ps -Arco pid= 2>/dev/null | head -n "$N"); do printf 'a\t%s\t%s\n' "$p" "$(ps -o args= -p "$p" 2>/dev/null | head -c 300 | tr '\t\n' '  ')"; done
else
awk '/^MemTotal:/ {printf "mem_total_mb %d\n", $2/1024} /^MemAvailable:/ {printf "mem_avail_mb %d\n", $2/1024}' /proc/meminfo 2>/dev/null
echo "load $(cut -d' ' -f1 /proc/loadavg 2>/dev/null)"
echo "cores $(nproc 2>/dev/null || getconf _NPROCESSORS_ONLN 2>/dev/null)"
for r in cpu memory; do [ -r "/proc/pressure/$r" ] && sed "s/^/psi_$r /" "/proc/pressure/$r" 2>/dev/null; done
ticks() { cat /proc/[0-9]*/stat 2>/dev/null | awk '{p = $1; sub(/^.*\) /, ""); print p, $12 + $13}'; }
t0=$(date +%s%N 2>/dev/null); s1=$(ticks); sleep 1; t1=$(date +%s%N 2>/dev/null)
cpu=$( { echo "$s1"; echo "--"; ticks; } | awk '$1 == "--" {b = 1; next} !b {a[$1] = $2; next} ($1 in a) && $2 > a[$1] {print $1, $2 - a[$1]}' | sort -k2 -nr)
echo "clk $(getconf CLK_TCK 2>/dev/null || echo 100)"
case "$t0$t1" in *[!0-9]*|"") ;; *) echo "interval_ns $((t1 - t0))" ;; esac
[ -n "$cpu" ] && echo "$cpu" | sed 's/^/c /'
ps -eo pid=,ppid=,nlwp=,pcpu=,rss=,comm= 2>/dev/null | sed 's/^ */p /'
for p in $(echo "$cpu" | head -n "$N" | cut -d' ' -f1); do printf 'a\t%s\t%s\n' "$p" "$(head -c 300 "/proc/$p/cmdline" 2>/dev/null | tr '\0\t\n' '   ')"; done
if [ -n "$W" ] && [ -d "$W/wt" ]; then
find /proc/[0-9]*/cwd -maxdepth 0 -printf '%h %l\n' 2>/dev/null | awk -v d="$W/wt/" 'index($2, d) == 1 {sub("/proc/", "", $1); print "w " $1}'
fi
fi
while [ $# -ge 2 ]; do o="$1"; d="$2"; shift 2
case "$d" in /*) ;; "~/"*) d="$HOME/${d#\~/}" ;; *) d="$HOME/$d" ;; esac
for f in "$d"/*.lock; do
[ -f "$f" ] || continue
n=$(basename "$f" .lock)
if [ "$os" = Darwin ]; then
if perl -MFcntl=:flock -e 'open(F,"<",$ARGV[0]) or exit 1; exit(flock(F,LOCK_EX|LOCK_NB)?0:1)' "$f" 2>/dev/null; then echo "s $o $n 0 -"; else echo "s $o $n 1 -"; fi
else
i=$(stat -c %i "$f" 2>/dev/null)
h=$(awk -v i=":$i" '$2 != "->" && substr($6, length($6) - length(i) + 1) == i {print $5; exit}' /proc/locks 2>/dev/null)
if [ -n "$h" ]; then echo "s $o $n 1 $h"; else echo "s $o $n 0 -"; fi
fi
done
done
exit 0
`

// DefaultTopArgs is how many of the busiest processes get their command line read.
const DefaultTopArgs = 12

// SlotDir is one queue's slot lock directory on a machine.
type SlotDir struct {
	Owner string
	Dir   string // absolute, "~/"-relative or relative to the machine's home; validated by config
}

// ProbeRequest is one machine to probe.
type ProbeRequest struct {
	Transport []string // argv prefix the script is appended to (ssh, or sh -c for this machine)
	WorkDir   string   // heavyq's work dir there ("" when heavyq runs nothing there)
	Slots     []SlotDir
	Timeout   time.Duration
	TopArgs   int // command lines to read (0: DefaultTopArgs)
}

// RunProbe runs the probe script on one machine. An error means the machine is unreachable (or its answer
// unreadable) within the timeout; the caller shows it as such and goes on.
func RunProbe(req ProbeRequest) (Probe, error) {
	if len(req.Transport) == 0 {
		return Probe{}, errors.New("no transport")
	}
	n := req.TopArgs
	if n <= 0 {
		n = DefaultTopArgs
	}
	wd := req.WorkDir
	if wd == "" {
		wd = "-"
	}
	args := []string{wd, strconv.Itoa(n)}
	for _, s := range req.Slots {
		args = append(args, s.Owner, s.Dir)
	}
	out, err := remote.Transport(req.Transport).Output(req.Timeout, fleetProbeScript, args...)
	if err != nil {
		return Probe{}, err
	}
	return ParseProbe(out)
}

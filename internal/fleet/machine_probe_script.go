package fleet

// fleetProbeScript measures one machine for `heavyq fleet`, read-only. It runs under bash through the
// machine's transport (`ssh host`, or `sh -c` for this machine and the tests). Args: <work_dir|-> <N>
// <budget s> then <owner> <slot dir> pairs. It prints one fact per line after the marker "heavyq-fleet 1":
//
//	os, cores, load, mem_total_mb, mem_avail_mb    the machine (macOS: raw sysctl/vm_stat between darwin-begin/-end)
//	psi_cpu ... / psi_memory ...                    /proc/pressure lines, when the kernel has them
//	workdir <abs>                                   heavyq's work dir there, resolved
//	self <pid>                                      the script itself: it and its children are left out
//	s <owner> <slot> <0|1> <holder pid|->           each <slot dir>/*.lock: held or not, and by whom
//	p <pid> <ppid> <threads> <%cpu> <rss_kb> <comm> every live process (zombies are left out)
//	clk <hz>, interval_ns <ns>, c <pid> <ticks>     Linux: CPU ticks each process used over a 1 s sample; this,
//	                                                not ps (CPU time over the process's whole life), is its %CPU.
//	                                                The sample is timed by /proc/uptime, read by the same cat
//	                                                just before and after the stat files: its span is that of
//	                                                the reads, which on a loaded machine is seconds longer than
//	                                                the sleep (the second cat's fork and exec; timing the sleep
//	                                                inflated %CPU several times). A read of the whole table can
//	                                                itself take a second or two there, and not the same time
//	                                                twice, so each process's ticks are scaled from the span
//	                                                between its own two reads (placed by its position in each
//	                                                read) to interval_ns, the span between the reads' midpoints
//	a<TAB><pid><TAB><args>                          the command line of the N busiest
//	w <pid>                                         a process whose cwd is in <work_dir>/wt (a heavyq remote job)
//	done <step> / skip <step> <seconds>             a step ended, or was not started because <budget s> was spent
//	end                                             the whole probe ended
//
// The steps run cheapest first, and each one's facts count only once its "done" line arrived: base (the
// machine's numbers, a handful of reads and one fork), slots, then the expensive ones: procs, args, wt.
// On a loaded machine a fork can take half a second, so the script forks a fixed handful of times, never
// once per process: /proc/<pid>/stat is read by one cat per sample, parsed by one awk, and command lines
// are read with bash's own `read`. A task that exited (state Z or X) is dropped before anything else is
// read of it; a task in D or T state never has its command line read (a D task can hold the lock that read
// waits on). Command lines are read by one subshell whose lines the script waits for only until the budget
// is spent: a read of /proc/<pid>/cmdline waits for that process's memory map lock and was seen to hang for
// minutes on a loaded machine, so it is given up on (the subshell is killed) instead of holding the probe.
// Once the budget is spent no expensive step starts, so a slow machine still answers, partially.
//
// A slot is read without being taken on Linux: /proc/locks says whether the file's inode is locked, and by
// which pid. macOS has no /proc/locks, so a non-blocking flock(2) is tried and released at once (as the
// scheduler's own probe does) and the holder is unknown.
const fleetProbeScript = `W="$1"; N="$2"; B="$3"; shift 3
LC_ALL=C; export LC_ALL
case "$W" in -) W="" ;; /*) ;; *) W="$HOME/$W" ;; esac
left() { [ "$SECONDS" -lt "$B" ]; }
echo "heavyq-fleet 1"; echo "self $$"
case "$OSTYPE" in darwin*) os=Darwin ;; linux*) os=Linux ;; *) os=$(uname -s 2>/dev/null) ;; esac
echo "os ${os:-unknown}"
[ -n "$W" ] && echo "workdir $W"
if [ "$os" = Darwin ]; then
PATH="$PATH:/usr/sbin:/sbin:/usr/bin:/bin"
echo darwin-begin
sysctl -n hw.memsize hw.ncpu vm.loadavg 2>/dev/null
vm_stat 2>/dev/null
echo darwin-end
else
while read -r k v _; do case "$k" in MemTotal:) echo "mem_total_mb $((v / 1024))" ;; MemAvailable:) echo "mem_avail_mb $((v / 1024))" ;; esac; done 2>/dev/null < /proc/meminfo
read -r l _ 2>/dev/null < /proc/loadavg && echo "load $l"
echo "cores $(nproc 2>/dev/null || getconf _NPROCESSORS_ONLN 2>/dev/null)"
for r in cpu memory; do [ -r "/proc/pressure/$r" ] && while read -r l; do echo "psi_$r $l"; done 2>/dev/null < "/proc/pressure/$r"; done
fi
echo "done base"
while [ $# -ge 2 ]; do o="$1"; d="$2"; shift 2
case "$d" in /*) ;; "~/"*) d="$HOME/${d#\~/}" ;; *) d="$HOME/$d" ;; esac
fs=(); for f in "$d"/*.lock; do [ -f "$f" ] && fs+=("$f"); done
[ ${#fs[@]} -gt 0 ] || continue
if [ "$os" = Darwin ]; then
for f in "${fs[@]}"; do n=$(basename "$f" .lock)
if perl -MFcntl=:flock -e 'open(F,"<",$ARGV[0]) or exit 1; exit(flock(F,LOCK_EX|LOCK_NB)?0:1)' "$f" 2>/dev/null; then echo "s $o $n 0 -"; else echo "s $o $n 1 -"; fi
done
else
stat -c '%i %n' -- "${fs[@]}" 2>/dev/null | awk -v o="$o" 'FNR == NR { if ($2 != "->") { k = split($6, a, ":"); if (!(a[k] in h)) h[a[k]] = $5 } next }
{ n = substr($0, length($1) + 2); sub(/.*\//, "", n); sub(/\.lock$/, "", n); if ($1 in h) print "s", o, n, 1, h[$1]; else print "s", o, n, 0, "-" }' /proc/locks - 2>/dev/null
fi
done
echo "done slots"
if ! left; then echo "skip procs $SECONDS"; echo end; exit 0; fi
if [ "$os" = Darwin ]; then
out=$(ps -Arco pid=,ppid=,pcpu=,rss=,state=,comm= 2>/dev/null | awk -v n="$N" '$5 ~ /^[ZX]/ { next }
{ l = "p " $1 " " $2 " 0 " $3 " " $4; for (i = 6; i <= NF; i++) l = l " " $i; P[++k] = l; if (t < n && $5 !~ /^[UDTt]/) { top = top " " $1; t++ } }
END { print "t" top; for (i = 1; i <= k; i++) print P[i] }')
else
u=/proc/uptime
out=$( { cat $u /proc/[0-9]*/stat $u; sleep 1; cat $u /proc/[0-9]*/stat $u; } 2>/dev/null |
awk -v n="$N" -v pg="$(getconf PAGESIZE 2>/dev/null)" 'NF == 2 && $1 ~ /^[0-9]+\.[0-9]+$/ { b++; t[b] = $1; next }
$1 !~ /^[0-9]+$/ || !index($0, ") ") { next }
{ s = $0; sub(/^.*\) /, "", s); split(s, f, " "); if (b < 3) i = ++k1; else i = ++k2; if (f[1] == "Z" || f[1] == "X") next
  if (b < 3) { a[$1] = f[12] + f[13]; x[$1] = i; next }
  c = $0; sub(/^[^(]*\(/, "", c); sub(/\) [^)]*$/, "", c); gsub(/[ \t]+/, " ", c); if (c == "" || c == " ") c = "?"
  P[++k] = "p " $1 " " f[2] " " f[18] " 0 " int(f[22] * (pg > 0 ? pg : 4096) / 1024) " " c
  if (($1 in a) && f[12] + f[13] > a[$1]) { d[$1] = f[12] + f[13] - a[$1]; st[$1] = f[1]; y[$1] = i } }
END { iv = b == 4 ? (t[3] + t[4] - t[1] - t[2]) / 2 : 0
  if (iv > 0) for (p in d) { s = t[3] + (t[4] - t[3]) * y[p] / k2 - t[1] - (t[2] - t[1]) * x[p] / k1; if (s > 0) d[p] = d[p] * iv / s }
  top = "t"
  for (i = 0; i < n; i++) { m = ""; for (p in d) if (!(p in u) && (m == "" || d[p] > d[m])) m = p
    if (m == "") break; u[m] = 1; if (st[m] !~ /^[DTt]/) top = top " " m }
  print top; print "clk 100"
  if (iv > 0) printf "interval_ns %.0f\n", iv * 1e9
  for (i = 1; i <= k; i++) print P[i]
  for (p in d) printf "c %s %.2f\n", p, d[p] }')
fi
case "$out" in t*) ;; *) echo "skip procs $SECONDS"; echo end; exit 0 ;; esac
top=${out%%$'\n'*}; top=${top#t}
[ "$out" != "${out#*$'\n'}" ] && printf '%s\n' "${out#*$'\n'}"
echo "done procs"
if [ "$os" = Darwin ]; then
if [ -n "$top" ]; then l=${top# }; l=${l// /,}; ps -ww -o pid=,args= -p "$l" 2>/dev/null | while read -r p a; do printf 'a\t%s\t%s\n' "$p" "${a:0:300}"; done; fi
echo "done args"
else
r=1; while :; do t=$((B - SECONDS)); [ "$t" -gt 0 ] || { r=2; break; }
IFS= read -r -t "$t" l || { [ $? -gt 128 ] && r=2; break; }; [ "$l" = "done args" ] && r=0; printf '%s\n' "$l"
done < <(for p in $top; do
a=""; while IFS= read -r -d '' x || [ -n "$x" ]; do a+="$x "; x=""; [ ${#a} -ge 300 ] && break; done < "/proc/$p/cmdline"
a=${a//[$'\t\r\n']/ }; a=${a:0:300}; a=${a%"${a##*[! ]}"}; [ -n "$a" ] && printf 'a\t%s\t%s\n' "$p" "$a"
done 2>/dev/null; echo "done args")
[ "$r" = 2 ] && { [ -n "$!" ] && kill -9 "$!" 2>/dev/null; echo "skip args $SECONDS"; }
fi
if [ -n "$W" ] && [ -d "$W/wt" ] && [ "$os" != Darwin ]; then
if left; then
find /proc/[0-9]*/cwd -maxdepth 0 -printf '%h %l\n' 2>/dev/null | awk -v d="$W/wt/" 'index($2, d) == 1 {sub("/proc/", "", $1); print "w " $1}'
echo "done wt"
else echo "skip wt $SECONDS"; fi
fi
echo end
exit 0
`

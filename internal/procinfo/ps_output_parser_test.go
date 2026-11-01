package procinfo

import (
	"testing"
	"time"
)

// macPS is the shape of `ps -axo pid=,ppid=,rss=,time=,comm=` on macOS:
// right-aligned numbers, time as mm:ss.cc (minutes may pass 59), comm as the
// full executable path, which can contain spaces and parentheses, plus a
// zombie shown as "(name)" and kernel_task at pid 0.
const macPS = `    0     0  125440 612:03.45 kernel_task
    1     0   18176   1:39.27 /sbin/launchd
  101     1   13200     0:08.52 /usr/sbin/syslogd
  412     1   64992    12:41.90 /usr/libexec/UserEventAgent
 8120   412  412304  1234:56.78 /Applications/Google Chrome.app/Contents/MacOS/Google Chrome
 8127  8120  151232     3:00.01 /Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Framework.framework/Versions/126.0/Helpers/Google Chrome Helper (Renderer).app/Contents/MacOS/Google Chrome Helper (Renderer)
 9001     1       0     0:00.00 (sh)
 9100   412    5280     0:00.12 /bin/zsh
`

func TestParsePSMacFixture(t *testing.T) {
	got := parsePS([]byte(macPS))
	if len(got) != 8 {
		t.Fatalf("parsed %d processes, want 8: %+v", len(got), got)
	}
	cases := map[int]procSample{
		0:    {ppid: 0, rssKB: 125440, ticks: 612*6000 + 345, comm: "kernel_task"},
		1:    {ppid: 0, rssKB: 18176, ticks: 99*100 + 27, comm: "launchd"},
		101:  {ppid: 1, rssKB: 13200, ticks: 852, comm: "syslogd"},
		8120: {ppid: 412, rssKB: 412304, ticks: (1234*60+56)*100 + 78, comm: "Google Chrome"},
		8127: {ppid: 8120, rssKB: 151232, ticks: 18001, comm: "Google Chrome Helper (Renderer)"},
		9001: {ppid: 1, rssKB: 0, ticks: 0, comm: "(sh)"},
	}
	for pid, want := range cases {
		if got[pid] != want {
			t.Errorf("pid %d = %+v, want %+v", pid, got[pid], want)
		}
	}
}

func TestParsePSLinuxProcpsOutput(t *testing.T) {
	// procps on Linux: hh:mm:ss without a fraction, optional "dd-", comm truncated to 15
	got := parsePS([]byte("    1     0  11264 00:00:07 systemd\n  733     1  50112 1-02:03:04 code-insiders\n 2045   733   9120 00:00:00 tmux: server\n"))
	if len(got) != 3 || got[1].ticks != 700 || got[733].ticks != (86400+2*3600+3*60+4)*100 || got[2045].comm != "tmux: server" {
		t.Fatalf("got %+v", got)
	}
}

func TestParsePSSkipsMalformedLines(t *testing.T) {
	in := "\n   \nPID PPID RSS TIME COMM\n 12 1 100\n x 1 100 0:01.00 sh\n 13 1 -5 0:01.00 sh\n 14 -1 5 0:01.00 sh\n 15 1 100 0:01.00 ok\n"
	got := parsePS([]byte(in))
	if len(got) != 1 || got[15].comm != "ok" {
		t.Fatalf("only the well-formed line may survive: %+v", got)
	}
}

func TestParsePSKeepsProcessWithUnreadableTime(t *testing.T) {
	// the process still belongs to its tree (Descendants), just with no CPU
	got := parsePS([]byte(" 20 1 100 ??:?? tool\n"))
	if s, ok := got[20]; !ok || s.ticks != 0 || s.ppid != 1 || s.comm != "tool" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseCPUTime(t *testing.T) {
	ok := map[string]uint64{
		"0:00.00":     0,
		"0:07.25":     725,
		"0:07.5":      750,
		"0:07.257":    725, // extra digits truncate to centiseconds
		"1:02.03":     6203,
		"1234:56.78":  (1234*60+56)*100 + 78,
		"1:02:03":     (3600 + 120 + 3) * 100,
		"1:02:03.50":  (3600+120+3)*100 + 50,
		"00:00:07":    700,
		"1-02:03:04":  (86400 + 7200 + 180 + 4) * 100,
		"12-00:00:00": 12 * 86400 * 100,
		"42":          4200,
		"42.07":       4207,
	}
	for in, want := range ok {
		if got, valid := parseCPUTime(in); !valid || got != want {
			t.Errorf("parseCPUTime(%q) = %d, %v; want %d", in, got, valid, want)
		}
	}
	for _, in := range []string{"", "-", ":", "1:", ":30", "1:2:3:4", "a:bc", "1:02.x", "-1:00", "+1:00", "1e3:00",
		"1-", "x-1:00", "99999999999:00:00", "1:-2", "0:00. 5"} {
		if got, valid := parseCPUTime(in); valid {
			t.Errorf("parseCPUTime(%q) = %d, want invalid", in, got)
		}
	}
}

func TestPSAttributeAndBreakdownUseTheSharedLogic(t *testing.T) {
	a := parsePS([]byte(" 10 1 1000 0:01.00 /usr/bin/heavyq\n 11 10 4096 0:01.00 /bin/zsh\n 2 1 10 0:00.00 /usr/libexec/real-pid-two\n 30 1 2048 0:05.00 /usr/bin/claude\n"))
	b := parsePS([]byte(" 10 1 1000 0:01.10 /usr/bin/heavyq\n 11 10 4096 0:02.00 /bin/zsh\n 2 1 10 0:00.50 /usr/libexec/real-pid-two\n 30 1 2048 0:05.40 /usr/bin/claude\n"))
	got := attribute(a, b, map[int]string{10: "lane api"}, "outside", time.Second)
	if len(got) != 2 || got[0].Group != "lane api" || got[0].Percent != 110 || got[1].Percent != 90 {
		t.Fatalf("attribute = %+v", got)
	}
	byRoot, outside := breakdownSkipping(a, b, map[int]string{10: "t"}, time.Second, isKernelTask)
	if len(byRoot) != 1 || byRoot[0].Procs != 2 || byRoot[0].CPUPct != 110 {
		t.Fatalf("byRoot = %+v", byRoot)
	}
	// pid 2 is an ordinary process on macOS: only pid 0 (kernel_task) is skipped
	names := map[string]bool{}
	for _, g := range outside {
		names[g.Group] = true
	}
	if !names["real-pid-two"] || !names["claude"] {
		t.Fatalf("outside = %+v", outside)
	}
}

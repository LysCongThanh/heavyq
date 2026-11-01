package metrics

import (
	"runtime"
	"testing"
)

// Fixtures are verbatim shapes of macOS output (Apple silicon: 16 KiB
// pages; the vm_stat numbers are from a 16 GiB machine under normal use).
const (
	sysctlFixture = "17179869184\n8\n{ 2.34 2.10 1.98 }\n"

	vmStatApple = `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                                5061.
Pages active:                            210522.
Pages inactive:                          199893.
Pages speculative:                         4312.
Pages throttled:                              0.
Pages wired down:                         87106.
Pages purgeable:                           9731.
"Translation faults":                 903441215.
Pages copy-on-write:                   18390412.
Pages zero filled:                    502901123.
Pages reactivated:                      4821903.
Pages purged:                           2210334.
File-backed pages:                       118902.
Anonymous pages:                         296811.
Pages stored in compressor:              301442.
Pages occupied by compressor:            120374.
Decompressions:                        88120943.
Compressions:                         121903744.
Pageins:                                9021122.
Pageouts:                                 11823.
Swapins:                                  40122.
Swapouts:                                 52088.
`

	vmStatIntel = `Mach Virtual Memory Statistics: (page size of 4096 bytes)
Pages free:                              321654.
Pages active:                           1950123.
Pages inactive:                         1240001.
Pages speculative:                        26788.
Pages throttled:                              0.
Pages wired down:                        645331.
Pages purgeable:                          54321.
"Translation faults":                1209334001.
`
)

func TestParseSysctl(t *testing.T) {
	mem, ncpu, load, err := parseSysctl(sysctlFixture)
	if err != nil || mem != 17179869184 || ncpu != 8 || load != 2.34 {
		t.Fatalf("got %d %d %v %v", mem, ncpu, load, err)
	}
	// older releases append the scale factor after the three averages
	if _, _, load, err := parseSysctl("8589934592\n4\n{ 0.50 0.60 0.70 } 2048\n"); err != nil || load != 0.5 {
		t.Fatalf("load with scale: %v %v", load, err)
	}
}

func TestParseSysctlRejectsBadOutput(t *testing.T) {
	for name, in := range map[string]string{
		"empty":        "",
		"two lines":    "17179869184\n8\n",
		"zero memory":  "0\n8\n{ 1.00 1.00 1.00 }\n",
		"text memory":  "lots\n8\n{ 1.00 1.00 1.00 }\n",
		"zero cpus":    "17179869184\n0\n{ 1.00 1.00 1.00 }\n",
		"empty load":   "17179869184\n8\n{ }\n",
		"garbage load": "17179869184\n8\n{ x y z }\n",
	} {
		if _, _, _, err := parseSysctl(in); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestParseVMStatSumsReclaimablePages(t *testing.T) {
	// (5061 free + 199893 inactive + 9731 purgeable + 4312 speculative) * 16 KiB
	got, err := parseVMStat(vmStatApple)
	if want := uint64(5061+199893+9731+4312) * 16384; err != nil || got != want {
		t.Fatalf("apple silicon: got %d, want %d, err %v", got, want, err)
	}
	// 4 KiB pages: the page size comes from the header, not from the host
	got, err = parseVMStat(vmStatIntel)
	if want := uint64(321654+1240001+54321+26788) * 4096; err != nil || got != want {
		t.Fatalf("intel: got %d, want %d, err %v", got, want, err)
	}
}

func TestParseVMStatRejectsBadOutput(t *testing.T) {
	for name, in := range map[string]string{
		"empty":          "",
		"no header":      "Pages free: 10.\nPages inactive: 10.\n",
		"no free line":   "Mach Virtual Memory Statistics: (page size of 4096 bytes)\nPages inactive: 10.\n",
		"no inactive":    "Mach Virtual Memory Statistics: (page size of 4096 bytes)\nPages free: 10.\n",
		"zero page size": "Mach Virtual Memory Statistics: (page size of 0 bytes)\nPages free: 1.\nPages inactive: 1.\n",
	} {
		if _, err := parseVMStat(in); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestHostIsLocalOffMacOS(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Host reads sysctl/vm_stat on macOS; see TestHostMacOS")
	}
	if m := Host("/definitely/not/proc"); m.Known {
		t.Fatalf("Host must follow procRoot off macOS, got %+v", m)
	}
}

func TestHostMacOS(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only: needs sysctl and vm_stat")
	}
	m := Host("/proc") // procRoot is ignored on macOS
	if !m.Known || m.TotalMB <= 0 || m.FreeMB <= 0 || m.FreeMB > m.TotalMB || m.Cores < 1 || m.Load1 < 0 {
		t.Fatalf("real Mac: %+v", m)
	}
}

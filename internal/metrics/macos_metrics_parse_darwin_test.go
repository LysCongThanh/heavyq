package metrics

import "testing"

func TestParseDarwinCombinesSysctlAndVMStat(t *testing.T) {
	m, err := ParseDarwin(sysctlFixture, vmStatApple)
	// (5061 + 199893 + 9731 + 4312) pages * 16 KiB = 3421 MiB, of 16384 MiB installed
	if err != nil || !m.Known || m.TotalMB != 16384 || m.FreeMB != 3421 || m.Cores != 8 || m.Load1 != 2.34 {
		t.Fatalf("got %+v, %v", m, err)
	}
	if _, err := ParseDarwin(sysctlFixture, ""); err == nil {
		t.Fatal("empty vm_stat must be an error")
	}
	if _, err := ParseDarwin("", vmStatApple); err == nil {
		t.Fatal("empty sysctl must be an error")
	}
}

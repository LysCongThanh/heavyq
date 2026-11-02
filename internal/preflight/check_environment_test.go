package preflight

import "testing"

func TestCheckEnvironment(t *testing.T) {
	local := MachineFact{Name: "local", Local: true, Reachable: true, HeavySlots: 2}
	linuxi := MachineFact{Name: "linuxi", SSH: "linuxi", Reachable: true, HeavySlots: 4}
	cases := []struct {
		name string
		in   EnvInput
		want []string
	}{
		{"all good", EnvInput{Parallel: 3, DiskKnown: true, DiskFreeBytes: 50 << 30, Machines: []MachineFact{local, linuxi}}, nil},
		{"disk full", EnvInput{DiskKnown: true, DiskFreeBytes: 200 << 20, DiskPath: "/r"}, []string{"block:only 0.2 GiB free"}},
		{"disk low", EnvInput{DiskKnown: true, DiskFreeBytes: 3 << 30, DiskPath: "/r"}, []string{"warn:3.0 GiB free"}},
		{"disk unknown is silent", EnvInput{DiskFreeBytes: 0}, nil},
		{"remote down", EnvInput{Parallel: 1, Machines: []MachineFact{local, {Name: "mac", SSH: "mac", Err: "timeout"}}},
			[]string{"warn:remote mac did not answer (timeout)"}},
		{"too few slots", EnvInput{Parallel: 5, Machines: []MachineFact{local}},
			[]string{"warn:5 lanes planned but 2 heavy slots in all (local 2)"}},
		{"remote slots count when it answers", EnvInput{Parallel: 5, Machines: []MachineFact{local, linuxi}}, nil},
		{"a dead remote's slots do not count", EnvInput{Parallel: 5, Machines: []MachineFact{local, {Name: "linuxi", Err: "x", HeavySlots: 4}}},
			[]string{"warn:remote linuxi did not answer", "warn:5 lanes planned but 2 heavy slots"}},
		{"small pool", EnvInput{Parallel: 3, Machines: []MachineFact{local, linuxi}, Pools: []PoolFact{{"pg", 1}, {"port", 100}}},
			[]string{"warn:pool pg holds 1 values for 3 lanes"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { assertFindings(t, CheckEnvironment(c.in), c.want) })
	}
}

func TestRemoteFixIsTheSSHCommand(t *testing.T) {
	fs := CheckEnvironment(EnvInput{Machines: []MachineFact{{Name: "mac", SSH: "mac-host"}}})
	if len(fs) != 1 || fs[0].Fix != "ssh mac-host true" {
		t.Errorf("%+v", fs)
	}
}

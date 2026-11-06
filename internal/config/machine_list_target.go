package config

import (
	"fmt"
	"strings"
)

// A placement target (--remote, HEAVYQ_REMOTE, [proof] remote, a gate's
// remote) is "local", "auto", the name of a [[remote]], or an ordered list of
// machine names separated by commas, e.g. "mac,local,linuxi": the run goes to
// the first machine of the list that has a free slot and passes the resource
// gate. See docs/remote.md.

// machineListSep separates the machines of a list target.
const machineListSep = ","

// IsMachineList reports whether v is a list target ("a,b"), whatever its shape.
func IsMachineList(v string) bool { return strings.Contains(v, machineListSep) }

// MachineList returns the machines of a list target in order, or nil when v
// is not a list. It does not validate them: see CheckMachineList.
func MachineList(v string) []string {
	if !IsMachineList(v) {
		return nil
	}
	return strings.Split(v, machineListSep)
}

// TargetMayRunLocally reports whether target lets a run stay on this machine:
// local, auto, or a list that names local. Anything else (one remote, or a
// list of remotes only) never runs here.
func TargetMayRunLocally(target string) bool {
	if target == LocalName || target == RemoteAuto {
		return true
	}
	for _, m := range MachineList(target) {
		if m == LocalName {
			return true
		}
	}
	return false
}

// CheckMachineList checks the shape of a list target: at least two entries,
// each "local" or a name a [[remote]] may have, none twice, no "auto" (a list
// is an explicit order), no empty entry and no spaces. It returns the
// machines in order. Whether each remote is configured is the caller's check.
func CheckMachineList(v string) ([]string, error) {
	ms := MachineList(v)
	if len(ms) < 2 {
		return nil, fmt.Errorf("%q is not a list of machines (a,b)", v)
	}
	seen := map[string]bool{}
	for _, m := range ms {
		switch {
		case m == RemoteAuto:
			return nil, fmt.Errorf("%q: %q cannot be in a list (a list is an explicit order; use %q alone)", v, RemoteAuto, RemoteAuto)
		case m != LocalName && !SafeName.MatchString(m):
			return nil, fmt.Errorf("%q: %q is not %q or a remote name (letters, digits, . _ -; no spaces)", v, m, LocalName)
		case seen[m]:
			return nil, fmt.Errorf("%q: %q is listed twice", v, m)
		}
		seen[m] = true
	}
	return ms, nil
}

package fleet

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/remote"
)

// DefaultTopArgs is how many of the busiest processes get their command line read.
const DefaultTopArgs = 12

// SlotDir is one queue's slot lock files on a machine: the files of Dir that match Pattern.
type SlotDir struct {
	Owner   string
	Dir     string // absolute, "~/"-relative or relative to the machine's home; validated by config
	Pattern string // base-name glob ("" means "*.lock", every lock file of a slot dir); validated by config
}

// SlotGlob is the SlotDir of a config slot glob (config.CheckSlotGlob): its directory and its base name.
func SlotGlob(owner, glob string) SlotDir {
	i := strings.LastIndex(glob, "/")
	dir := glob[:i]
	switch dir {
	case "":
		dir = "/"
	case "~":
		dir = "~/"
	}
	return SlotDir{Owner: owner, Dir: dir, Pattern: glob[i+1:]}
}

// ProbeRequest is one machine to probe.
type ProbeRequest struct {
	Transport []string // argv prefix the script is appended to (ssh, or sh -c for this machine)
	WorkDir   string   // heavyq's work dir there ("" when heavyq runs nothing there)
	Slots     []SlotDir
	Timeout   time.Duration
	TopArgs   int // command lines to read (0: DefaultTopArgs)
}

// probeBudget is how long, from its own start, the script may go on starting expensive steps: half the
// timeout, so that what it measured reaches heavyq before the timeout ends the transport (the connection
// itself takes part of the other half on a loaded machine).
func probeBudget(timeout time.Duration) int {
	return int((timeout / 2).Seconds())
}

// RunProbe runs the probe script on one machine. An error means the machine is unreachable (or its answer
// unreadable) within the timeout; the caller shows it as such and goes on. A machine that answered with
// its numbers (cores, load, RAM, pressure) but not with everything else in time is not an error: the probe
// keeps what arrived and says in Partial what is missing and why.
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
	args := []string{wd, strconv.Itoa(n), strconv.Itoa(probeBudget(req.Timeout))}
	for _, s := range req.Slots {
		pattern := s.Pattern
		if pattern == "" {
			pattern = "*.lock"
		}
		args = append(args, s.Owner, s.Dir, pattern)
	}
	out, err := remote.Transport(req.Transport).OutputPartial(req.Timeout, fleetProbeScript, args...)
	if err == nil {
		return parseProbe(out, "", req.WorkDir != "")
	}
	p, perr := parseProbe(out, err.Error(), req.WorkDir != "")
	if perr != nil {
		return Probe{}, err // not even the machine's numbers arrived: unreachable, with the transport's reason
	}
	return p, nil
}

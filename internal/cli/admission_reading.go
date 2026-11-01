package cli

import (
	"math"

	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/schedule"
)

// admissionReading is the machine as the scheduling round that admitted a
// ticket saw it: the same sample the resource gate judged (metrics.Local for
// the local machine, the probe placement used for a remote), plus the limits
// the gate applied. It is written to the ledger so a failure can later be told
// apart from one that happened while the machine was overloaded. The zero
// value means "not measured".
type admissionReading struct {
	load1   float64
	freeMB  int
	cores   int
	maxLoad float64 // load1 at or above this is over the limit
	jobMB   int     // the job's RAM budget on that machine
}

// readingOf takes the reading of st for a ticket that is light or not. A
// machine that cannot be measured gives the zero reading: nothing is claimed
// about it.
func readingOf(st schedule.MachineState, light bool) admissionReading {
	m := st.Metrics
	if !m.Known || m.Cores <= 0 {
		return admissionReading{}
	}
	job := st.Cfg.JobMB
	if light {
		job = st.Cfg.LightMB
	}
	return admissionReading{
		load1: m.Load1, freeMB: m.FreeMB, cores: m.Cores, jobMB: job,
		maxLoad: math.Round(st.Cfg.MaxLoadRatio*float64(m.Cores)*100) / 100,
	}
}

// stampAdmission writes the machine reading taken at admission onto the
// ledger record of the run that was admitted by c.
func (c *claim) stampAdmission(rec *history.Record) {
	a := c.reading
	rec.Load1, rec.FreeMB, rec.Cores, rec.MaxLoad, rec.JobMB = a.load1, a.freeMB, a.cores, a.maxLoad, a.jobMB
}

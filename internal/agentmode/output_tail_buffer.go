package agentmode

// tail keeps the last max bytes written to it. It slides over a slice that is
// compacted whenever it reaches twice max, so memory stays below 2*max and a
// write is amortised O(len).
type tail struct {
	max   int
	data  []byte
	total int64 // bytes ever written
}

func (t *tail) write(p []byte) {
	t.total += int64(len(p))
	if len(p) >= t.max {
		t.data = append(t.data[:0], p[len(p)-t.max:]...)
		return
	}
	t.data = append(t.data, p...)
	if len(t.data) >= 2*t.max {
		n := copy(t.data, t.data[len(t.data)-t.max:])
		t.data = t.data[:n]
	}
}

// bytes returns the retained tail (a copy) and whether anything before it was dropped.
func (t *tail) bytes() ([]byte, bool) {
	d := t.data
	if len(d) > t.max {
		d = d[len(d)-t.max:]
	}
	return append([]byte(nil), d...), t.total > int64(len(d))
}

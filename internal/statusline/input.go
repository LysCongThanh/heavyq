// Package statusline prints heavyq's segment of Claude Code's status line and
// keeps it honest about its budget: Claude Code debounces the status line at
// 300 ms and a slow script blocks updates, so everything here reads files that
// already exist (the materialised flow state), never runs git and never
// computes flow. The work that is not cheap is handed off: a stale state starts
// a detached `heavyq flow refresh`, a notice is delivered by a detached process.
package statusline

import (
	"encoding/json"
	"io"
	"os"
	"time"
)

// maxInput bounds the status-line JSON read from stdin (a few KB in practice).
const maxInput = 1 << 20

// readTimeout is how long to wait for stdin when nothing ever closes it.
const readTimeout = 200 * time.Millisecond

// Input is the part of Claude Code's status-line JSON heavyq uses. Every field
// is optional and read on its own: a field of an unexpected type loses only
// itself, never the whole input.
type Input struct {
	CWD       string
	Model     string // model.id
	Version   string // the agent's version
	SessionID string
	FiveHour  int // rate_limits.five_hour used percentage; -1 when absent
	SevenDay  int // rate_limits.seven_day used percentage; -1 when absent
}

// Dir is where the session is working: workspace.current_dir, else cwd.
func (in Input) Dir() string { return in.CWD }

// ParseInput reads the status-line JSON; garbage gives an empty Input.
func ParseInput(b []byte) Input {
	in := Input{FiveHour: -1, SevenDay: -1}
	var top map[string]json.RawMessage
	if json.Unmarshal(b, &top) != nil {
		return in
	}
	str := func(raw json.RawMessage) string {
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	}
	sub := func(raw json.RawMessage) map[string]json.RawMessage {
		var m map[string]json.RawMessage
		_ = json.Unmarshal(raw, &m)
		return m
	}
	in.CWD = str(top["cwd"])
	if d := str(sub(top["workspace"])["current_dir"]); d != "" {
		in.CWD = d
	}
	in.Model = str(sub(top["model"])["id"])
	in.Version = str(top["version"])
	in.SessionID = str(top["session_id"])
	rl := sub(top["rate_limits"])
	in.FiveHour = percent(rl["five_hour"])
	in.SevenDay = percent(rl["seven_day"])
	return in
}

// percent reads a rate-limit window: {"used_percentage": 61.3, ...}, or a bare number.
func percent(raw json.RawMessage) int {
	var n float64
	if json.Unmarshal(raw, &n) == nil {
		return clampPct(n)
	}
	var w struct {
		Used *float64 `json:"used_percentage"`
	}
	if json.Unmarshal(raw, &w) == nil && w.Used != nil {
		return clampPct(*w.Used)
	}
	return -1
}

func clampPct(f float64) int {
	switch {
	case f < 0:
		return 0
	case f > 100:
		return 100
	}
	return int(f + 0.5)
}

// ReadInput reads stdin without ever hanging the status line: nothing is read
// from a terminal, and a pipe that never closes is abandoned after readTimeout.
func ReadInput(r io.Reader) []byte {
	if f, ok := r.(*os.File); ok {
		if st, err := f.Stat(); err != nil || st.Mode()&os.ModeCharDevice != 0 {
			return nil
		}
	}
	done := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(io.LimitReader(r, maxInput))
		done <- b
	}()
	select {
	case b := <-done:
		return b
	case <-time.After(readTimeout):
		return nil
	}
}

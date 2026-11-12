package fleet

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"
)

// DefaultHistoryMaxBytes bounds how much of a foreign history is read per report: the tail is read
// backwards from the end, so a file that grows for years costs what the window holds, never more than this.
const DefaultHistoryMaxBytes = 32 << 20

// clockSkew is how far out of order lines may arrive: several machines append when their runs end, so a line
// slightly older than the window can sit after newer ones. Reading stops only past this slack.
const clockSkew = time.Hour

// historyChunk is how much is read per step backwards.
const historyChunk = 64 << 10

// HistorySource says which history to read and how far back.
type HistorySource struct {
	Path     string
	Columns  []string  // the column names in file order (config.ForeignColumns layout or a permutation)
	Since    time.Time // stop at the first line (going backwards) that ended before it; zero: no bound
	MaxBytes int64     // stop after reading this much (0: DefaultHistoryMaxBytes)
}

// HistoryRead is the tail of one history.
type HistoryRead struct {
	Runs      []Run // oldest first; Queue and Class are left to the caller
	Skipped   int   // lines that did not parse (torn writes, comments, a missing exit)
	Truncated bool  // MaxBytes was reached before the window's start: the window is partial
}

// ReadHistoryTSV reads the newest lines of a tab-separated history. Lines are in append order, which is the
// order runs ended in, so the first line older than Since ends the read. A missing file is empty.
func ReadHistoryTSV(src HistorySource) (HistoryRead, error) {
	var out HistoryRead
	f, err := os.Open(src.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return out, err
	}
	limit := src.MaxBytes
	if limit <= 0 {
		limit = DefaultHistoryMaxBytes
	}
	idx := columnIndex(src.Columns)
	var rev []Run
	var carry []byte // the start of a line whose beginning is in the next (earlier) chunk
	pos, read := st.Size(), int64(0)
	done := false
	for pos > 0 && !done {
		if read >= limit {
			out.Truncated = true
			break
		}
		n := min(int64(historyChunk), pos, limit-read)
		pos -= n
		read += n
		buf := make([]byte, n, n+int64(len(carry)))
		if _, err := f.ReadAt(buf, pos); err != nil && err != io.EOF {
			return out, err
		}
		buf = append(buf, carry...)
		lines := bytes.Split(buf, []byte{'\n'})
		carry = lines[0] // may be cut: kept for the next chunk, or read whole at the start of the file
		for i := len(lines) - 1; i >= 1 && !done; i-- {
			done = takeLine(lines[i], idx, src.Since, &rev, &out)
		}
	}
	if pos == 0 && !done && len(carry) > 0 {
		takeLine(carry, idx, src.Since, &rev, &out)
	}
	for i := len(rev) - 1; i >= 0; i-- {
		out.Runs = append(out.Runs, rev[i])
	}
	return out, nil
}

// takeLine parses one line into rev; it reports true when the line is older than since (stop reading).
func takeLine(line []byte, idx map[string]int, since time.Time, rev *[]Run, out *HistoryRead) bool {
	s := strings.TrimRight(string(line), "\r")
	if strings.TrimSpace(s) == "" {
		return false
	}
	r, ok := parseHistoryLine(strings.Split(s, "\t"), idx)
	if !ok {
		out.Skipped++
		return false
	}
	if !since.IsZero() && r.End.Before(since) {
		return r.End.Before(since.Add(-clockSkew)) // inside the slack: skip the line, keep reading
	}
	*rev = append(*rev, r)
	return false
}

func columnIndex(cols []string) map[string]int {
	idx := map[string]int{}
	for i, c := range cols {
		idx[c] = i
	}
	return idx
}

// parseHistoryLine reads the known columns; end_epoch, machine and exit must parse, the others default.
func parseHistoryLine(f []string, idx map[string]int) (Run, bool) {
	get := func(col string) (string, bool) {
		i, ok := idx[col]
		if !ok || i >= len(f) {
			return "", false
		}
		return strings.TrimSpace(f[i]), true
	}
	var r Run
	end, ok1 := get("end_epoch")
	machine, ok2 := get("machine")
	exit, ok3 := get("exit")
	sec, err1 := strconv.ParseInt(end, 10, 64)
	code, err2 := strconv.Atoi(exit)
	if !ok1 || !ok2 || !ok3 || err1 != nil || err2 != nil || machine == "" || code < 0 || code > 255 || sec < 1e9 {
		return r, false
	}
	r.End, r.Machine, r.Exit = time.Unix(sec, 0), machine, code
	r.Lane, _ = get("lane")
	if v, ok := get("prio"); ok {
		r.Prio, _ = strconv.Atoi(v)
	}
	if v, ok := get("light"); ok {
		r.Light = v == "1" || v == "true"
	}
	if v, ok := get("wait_s"); ok {
		r.WaitS, _ = strconv.ParseFloat(v, 64)
		if !(r.WaitS >= 0) { // negative or NaN
			return r, false
		}
	}
	if v, ok := get("run_s"); ok {
		r.RunS, _ = strconv.ParseFloat(v, 64)
		if !(r.RunS >= 0) {
			return r, false
		}
	}
	return r, true
}

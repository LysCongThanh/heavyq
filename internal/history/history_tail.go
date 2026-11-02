package history

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"
)

// tailChunk is how much of a file is read per step backwards (a variable so
// tests can make chunk boundaries fall inside lines).
var tailChunk int64 = 64 * 1024

// TailLimit bounds ReadTail. The zero value reads everything.
type TailLimit struct {
	// Max is the most records returned (0: no limit).
	Max int
	// Since stops the read at the first record (going backwards) that ended
	// before it. Lines are in append order, which is time order up to clock
	// steps, so one older record means the rest is older too. Zero: no bound.
	Since time.Time
}

// ReadTail returns the newest records of path, oldest first, reading backwards
// from the end of the newest file and into older files of the rotated set (see
// files) only while the limit is not reached. The cost follows what is
// returned, not the size of the ledger: this is the read for hot paths. Corrupt
// and oversized lines are skipped like ReadLast does, a last line without a
// line ending is read like any other (a torn write is corrupt and skipped), and
// a missing file is empty.
func ReadTail(path string, lim TailLimit) ([]Record, error) {
	paths, err := files(path)
	if err != nil {
		return nil, err
	}
	rev := make([]Record, 0, min(max(lim.Max, 16), 4096)) // newest first
	done := false
	for i := len(paths) - 1; i >= 0 && !done; i-- {
		err := eachLineBackwards(paths[i], func(line []byte) bool {
			r, err := decodeRecord(line)
			if err != nil {
				return true
			}
			if !lim.Since.IsZero() && r.End.Before(lim.Since) {
				done = true
				return false
			}
			rev = append(rev, r)
			if lim.Max > 0 && len(rev) >= lim.Max {
				done = true
			}
			return !done
		})
		if err != nil {
			return reversed(rev), err
		}
	}
	return reversed(rev), nil
}

func reversed(rev []Record) []Record {
	if len(rev) == 0 {
		return nil
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

// eachLineBackwards calls fn for the lines of the file at path from the last to
// the first, without their line endings, until fn returns false. Empty lines
// are not reported and a line longer than maxLineBytes is dropped whole. The
// slice is only valid during the call. A missing file has no lines.
func eachLineBackwards(path string, fn func(line []byte) bool) error {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var (
		carry    []byte // the end of a line whose start is in an earlier chunk
		skipping bool   // inside an oversized line: drop bytes until its start
		buf      = make([]byte, tailChunk)
	)
	for pos := size; pos > 0; {
		n := min(tailChunk, pos)
		pos -= n
		if _, err := f.ReadAt(buf[:n], pos); err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		chunk := buf[:n]
		if skipping {
			i := bytes.LastIndexByte(chunk, '\n')
			if i < 0 {
				continue // still inside the oversized line
			}
			chunk, skipping = chunk[:i], false // the line before that newline is whole
		}
		data := chunk
		if len(carry) > 0 {
			data = append(append(make([]byte, 0, len(chunk)+len(carry)), chunk...), carry...)
		}
		end := len(data)
		for {
			i := bytes.LastIndexByte(data[:end], '\n')
			if i < 0 {
				break
			}
			if !emit(data[i+1:end], fn) {
				return nil
			}
			end = i
		}
		carry = append(carry[:0], data[:end]...) // the start of the line cut by this chunk
		if len(carry) > maxLineBytes {
			carry, skipping = carry[:0], true
		}
	}
	if !skipping {
		emit(carry, fn) // the first line of the file
	}
	return nil
}

// emit reports one line to fn unless it is empty or over the limit. It returns
// false when fn asked to stop.
func emit(line []byte, fn func([]byte) bool) bool {
	line = bytes.TrimRight(line, "\r")
	if len(line) == 0 || len(line) > maxLineBytes {
		return true
	}
	return fn(line)
}

package history

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Append writes r to path as one line, stamping the current schema and a new
// id when r has none. It is a single small O_APPEND write, which is atomic
// enough for concurrent writers on local filesystems.
func Append(path string, r Record) error {
	if path == "" {
		return nil
	}
	r.V = SchemaVersion
	if r.ID == "" {
		t := r.End
		if t.IsZero() {
			t = time.Now()
		}
		r.ID = NewID(t)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ReadLast returns up to n most recent records (oldest first; n <= 0 means
// all). For a rotated path (see files) it spans the files, newest last.
// Corrupt lines are skipped. Lines of every schema version load.
func ReadLast(path string, n int) ([]Record, error) {
	paths, err := files(path)
	if err != nil {
		return nil, err
	}
	var out []Record
	for i := len(paths) - 1; i >= 0; i-- {
		recs, err := readFile(paths[i], n)
		if len(recs) > 0 {
			out = append(append(make([]Record, 0, len(recs)+len(out)), recs...), out...)
		}
		if err != nil {
			return lastN(out, n), err
		}
		if n > 0 && len(out) >= n {
			break // older files cannot contribute to the last n
		}
	}
	return lastN(out, n), nil
}

// readFile returns up to the last n records of one file (all when n <= 0).
// A missing file is empty.
func readFile(path string, n int) ([]Record, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Record
	err = eachLine(f, func(line []byte) {
		var r Record
		if json.Unmarshal(line, &r) == nil {
			out = append(out, r)
			if n > 0 && len(out) >= 2*n { // bound memory on a long file
				out = append(out[:0], out[len(out)-n:]...)
			}
		}
	})
	if err != nil {
		return lastN(out, n), fmt.Errorf("read %s: %w", path, err)
	}
	return lastN(out, n), nil
}

// maxLineBytes is the longest ledger line that is decoded. A real line is a
// few hundred bytes (the command is truncated before it is stored).
const maxLineBytes = 1024 * 1024

// eachLine calls fn for every line of r (without its line ending). A line
// longer than maxLineBytes is corrupt like any other: it is skipped and the
// lines after it still load. Only a read error stops the walk.
func eachLine(r io.Reader, fn func(line []byte)) error {
	br := bufio.NewReaderSize(r, 64*1024)
	var buf []byte
	skipping := false
	for {
		chunk, err := br.ReadSlice('\n')
		if !skipping {
			buf = append(buf, chunk...)
			if len(buf) > maxLineBytes {
				buf, skipping = nil, true
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue // the line goes on in the next chunk
		}
		if !skipping && len(buf) > 0 {
			fn(bytes.TrimRight(buf, "\r\n"))
		}
		buf, skipping = buf[:0], false
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func lastN(recs []Record, n int) []Record {
	if n > 0 && len(recs) > n {
		return recs[len(recs)-n:]
	}
	return recs
}

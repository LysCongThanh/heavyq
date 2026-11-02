package history

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// Files lists the ledger files that path stands for, oldest first: path itself
// for a pinned file, else the pre-rotation history.jsonl and every monthly file
// of its directory (see files). Files that do not exist yet are not listed.
func Files(path string) ([]string, error) {
	ps, err := files(path)
	if err != nil {
		return nil, err
	}
	out := ps[:0]
	for _, p := range ps {
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out, nil
}

// EachFrom calls fn for every record of the single ledger file at path from
// byte offset on, oldest first, and returns the size of the file it read up to
// (the offset to continue from next time). Corrupt and oversized lines are
// skipped. A missing file is empty (size 0); an offset past the end of the file
// is an error, because the file is not the one the offset was taken from.
func EachFrom(path string, offset int64, fn func(Record)) (int64, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		if offset > 0 {
			return 0, fmt.Errorf("read %s: file is gone (was %d bytes)", path, offset)
		}
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := fi.Size()
	if offset > size {
		return size, fmt.Errorf("read %s: file shrank from %d to %d bytes", path, offset, size)
	}
	err = eachLine(io.NewSectionReader(f, offset, size-offset), func(line []byte) {
		if r, err := decodeRecord(line); err == nil {
			fn(r)
		}
	})
	if err != nil {
		return size, fmt.Errorf("read %s: %w", path, err)
	}
	return size, nil
}

// Each calls fn for every record of the files path stands for (see Files),
// oldest first, without holding them in memory. It returns the file and size
// the walk ended at: the newest file read, and its size at that time.
func Each(path string, fn func(Record)) (last string, size int64, err error) {
	ps, err := Files(path)
	if err != nil {
		return "", 0, err
	}
	for _, p := range ps {
		if size, err = EachFrom(p, 0, fn); err != nil {
			return p, size, err
		}
		last = p
	}
	return last, size, nil
}

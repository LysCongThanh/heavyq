package runspan

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func read(p string) (Span, error) {
	f, err := os.Open(p)
	if err != nil {
		return Span{}, err
	}
	defer f.Close()
	var s Span
	if err := json.NewDecoder(io.LimitReader(f, maxSpanBytes)).Decode(&s); err != nil {
		return Span{}, fmt.Errorf("%s: %w", filepath.Base(p), err)
	}
	return s, nil
}

// write stores s atomically: a temp file in the same directory, then rename.
func write(common string, s Span) error {
	dir := Dir(common)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".span-*") // 0600 already; the dot keeps List from reading it
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(append(b, '\n'))
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, spanPath(common, s.ID)); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

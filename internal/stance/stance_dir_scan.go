package stance

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Warning is a "*.md" file in a stance directory that heavyq left alone
// because it is not a stance (a README, notes, a draft). It is not an error:
// a directory is a place people keep other things too. A file that looks like
// a stance and is broken is still an error, see scanDir.
type Warning struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

func (w Warning) String() string { return w.Path + ": ignored, " + w.Reason }

// sniffBytes is how much of a file is read to tell whether it starts with the
// +++ fence: the fence line plus any trailing whitespace on it.
const sniffBytes = 256

// scanDir lists the stance files of dir (full paths, sorted by name) and the
// "*.md" files it ignores. A file is a stance candidate when its name without
// ".md" is a valid stance name AND its first line is the +++ fence of the
// frontmatter. Anything else ending in ".md" is ignored with a Warning: the
// name pattern and the fence are what make a file look like a stance, and only
// a candidate is parsed, so only a candidate can fail loudly. Dot files, other
// extensions, directories and a missing dir are skipped silently. The error
// is for what cannot be read.
func scanDir(dir string) (files []string, warns []Warning, err error) {
	if dir == "" {
		return nil, nil, nil
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", dir, err)
	}
	var errs []error
	for _, e := range entries { // ReadDir sorts by name
		name := e.Name()
		if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") || e.IsDir() {
			continue
		}
		path := filepath.Join(dir, name)
		if !ValidName(strings.TrimSuffix(name, ".md")) {
			warns = append(warns, Warning{path, "its name is not a valid stance name (" + namePattern.String() + " plus .md)"})
			continue
		}
		fenced, err := startsWithFence(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		if !fenced {
			warns = append(warns, Warning{path, "it does not start with a +++ frontmatter line"})
			continue
		}
		files = append(files, path)
	}
	return files, warns, errors.Join(errs...)
}

// startsWithFence reports whether the first line of the file is "+++" (a UTF-8
// BOM and trailing whitespace are accepted, as Parse accepts them). It reads
// at most sniffBytes, so a large unrelated file costs nothing.
func startsWithFence(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, sniffBytes))
	if err != nil {
		return false, err
	}
	buf = bytes.TrimPrefix(buf, []byte("\xef\xbb\xbf"))
	line, _, found := bytes.Cut(buf, []byte("\n"))
	if !found && len(buf) == sniffBytes {
		return false, nil // a first line this long is no fence
	}
	return string(bytes.TrimRight(line, " \t\r")) == fence, nil
}

package stance

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/gitutil"
)

// Dirs are the directories stances are read from. Empty means "none".
type Dirs struct {
	Repo string // <git toplevel>/.heavyq/stances
	User string // $XDG_CONFIG_HOME/heavyq/stances or ~/.config/heavyq/stances
}

// DefaultDirs resolves the directories for a process running in cwd. Outside
// a git work tree there is simply no repo directory.
func DefaultDirs(cwd string) Dirs {
	var d Dirs
	if top, err := gitutil.TopLevel(cwd); err == nil && top != "" {
		d.Repo = filepath.Join(top, ".heavyq", "stances")
	}
	if base := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(base) {
		d.User = filepath.Join(base, "heavyq", "stances")
	} else if home, err := os.UserHomeDir(); err == nil && filepath.IsAbs(home) {
		d.User = filepath.Join(home, ".config", "heavyq", "stances")
	}
	return d
}

// Load returns every stance, sorted by name. Precedence for the same name:
// repo over user over built-in. Any invalid file is an error naming the
// file (and line), and all of them are reported at once: a broken override
// never silently falls back to the built-in one it was meant to replace.
// A missing directory is normal.
func Load(d Dirs) ([]Stance, error) {
	builtins, err := Builtins()
	if err != nil {
		return nil, err
	}
	byName := map[string]Stance{}
	for _, s := range builtins {
		byName[s.Name] = s
	}
	var errs []error
	for _, layer := range []struct {
		dir string
		src Source
	}{{d.User, SourceUser}, {d.Repo, SourceRepo}} { // later layers win
		found, err := loadDir(layer.dir, layer.src)
		errs = append(errs, err)
		for _, s := range found {
			byName[s.Name] = s
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	out := make([]Stance, 0, len(byName))
	for _, s := range byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Find returns the named stance from the loaded set.
func Find(all []Stance, name string) (Stance, bool) {
	for _, s := range all {
		if s.Name == name {
			return s, true
		}
	}
	return Stance{}, false
}

// loadDir parses every regular "*.md" file of dir (dot files and other
// extensions are ignored). Invalid files are joined into the error; the
// valid ones are still returned.
func loadDir(dir string, src Source) ([]Stance, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	var out []Stance
	var errs []error
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") || e.IsDir() {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := readBounded(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		s, err := Parse(path, data)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		s.Source, s.Path = src, path
		out = append(out, s)
	}
	return out, errors.Join(errs...)
}

// readBounded reads a regular file, refusing anything that cannot be a valid
// stance by size (frontmatter and body limits, plus the fences).
func readBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	const limit = MaxBodyBytes + maxFrontmatterBytes + 64
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("larger than %d bytes (body limit %d)", limit, MaxBodyBytes)
	}
	return data, nil
}

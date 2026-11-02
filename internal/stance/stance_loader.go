package stance

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

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
// repo over user over built-in. Any invalid stance file is an error naming
// the file (and line), and all of them are reported at once: a broken override
// never silently falls back to the built-in one it was meant to replace.
// A missing directory is normal, and so is a file that is no stance at all
// (a README, notes): it is ignored, see LoadReport.
func Load(d Dirs) ([]Stance, error) {
	all, _, err := loadLayers(d, true)
	return all, err
}

// LoadReport is Load that also returns the "*.md" files it ignored because
// they are not stances, for `heavyq stance ls`.
func LoadReport(d Dirs) ([]Stance, []Warning, error) { return loadLayers(d, true) }

// loadLayers is Load with the repo layer optional: without it (a repo whose
// stances are not trusted) the repo's files are not even read, and the
// stances of the lower layers stand.
func loadLayers(d Dirs, withRepo bool) ([]Stance, []Warning, error) {
	builtins, err := Builtins()
	if err != nil {
		return nil, nil, err
	}
	byName := map[string]Stance{}
	for _, s := range builtins {
		byName[s.Name] = s
	}
	var errs []error
	var warns []Warning
	for _, layer := range []struct {
		dir string
		src Source
	}{{d.User, SourceUser}, {d.Repo, SourceRepo}} { // later layers win
		if layer.src == SourceRepo && !withRepo {
			continue
		}
		found, w, err := loadDir(layer.dir, layer.src)
		errs, warns = append(errs, err), append(warns, w...)
		for _, s := range found {
			byName[s.Name] = s
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, nil, err
	}
	out := make([]Stance, 0, len(byName))
	for _, s := range byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, warns, nil
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

// loadDir parses every stance file of dir (scanDir says what counts as one).
// Invalid stance files are joined into the error; the valid ones are still
// returned, and the ignored "*.md" files come back as warnings.
func loadDir(dir string, src Source) ([]Stance, []Warning, error) {
	files, warns, scanErr := scanDir(dir)
	var out []Stance
	errs := []error{scanErr}
	for _, path := range files {
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
	return out, warns, errors.Join(errs...)
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

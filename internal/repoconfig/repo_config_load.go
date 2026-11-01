package repoconfig

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Load reads heavyq.toml from the git top level that contains dir.
//
// It returns (nil, nil) when dir is not inside a git work tree or the
// repository has no heavyq.toml: that is a normal state, not an error. An
// invalid file is an error that names the file (and the line when the TOML
// library or the unknown-key scan can tell).
func Load(dir string) (*Repo, error) {
	root, err := gitTopLevel(dir)
	if err != nil || root == "" {
		return nil, err
	}
	return loadFile(root)
}

// LoadOrDetect is Load, except that a repository without heavyq.toml gets the
// default gate Detect makes from the files at its root. A heavyq.toml, even an
// empty or invalid one, always wins: detection only fills the absence of a
// file. It returns (nil, nil) outside a work tree and when nothing is detected.
func LoadOrDetect(dir string) (*Repo, error) {
	root, err := gitTopLevel(dir)
	if err != nil || root == "" {
		return nil, err
	}
	repo, err := loadFile(root)
	if repo != nil || err != nil {
		return repo, err
	}
	return Detect(root), nil
}

// loadFile reads and validates root/heavyq.toml; (nil, nil) when it is absent.
func loadFile(root string) (*Repo, error) {
	path := filepath.Join(root, FileName)
	data, err := readBounded(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return parse(root, path, data)
}

// gitTopLevel returns the work tree root containing dir, or "" when dir is
// not in a work tree (not a repository, a bare repository, or git refusing
// the directory). Only a missing or unrunnable git binary is an error.
func gitTopLevel(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_OPTIONAL_LOCKS=0")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", nil
		}
		return "", fmt.Errorf("find git root of %s: %w", dir, err)
	}
	root := strings.TrimRight(out.String(), "\n")
	if root == "" || !filepath.IsAbs(root) {
		return "", nil
	}
	return root, nil
}

// readBounded reads path, refusing anything that is not a regular file or
// that is larger than maxFileBytes.
func readBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil {
		return nil, err
	} else if !fi.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileBytes {
		return nil, fmt.Errorf("larger than %d bytes", maxFileBytes)
	}
	return data, nil
}

// parse decodes and validates data, the content of path.
func parse(root, path string, data []byte) (*Repo, error) {
	var raw rawFile
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if undec := md.Undecoded(); len(undec) > 0 {
		key := undec[0]
		if line := unknownKeyLine(data, key); line > 0 {
			return nil, fmt.Errorf("%s:%d: unknown key %q", path, line, key.String())
		}
		return nil, fmt.Errorf("%s: unknown key %q", path, key.String())
	}
	repo := &Repo{Root: root, Path: path}
	if err := repo.fill(raw); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return repo, nil
}

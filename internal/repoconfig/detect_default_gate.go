package repoconfig

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// DetectedGateName is the name of the gate Detect makes. It is also the name
// `heavyq gate status` and `gate run` look for first.
const DetectedGateName = "ready"

// Detect makes the default gate of a repository that has no heavyq.toml, from
// the files at its git root, in this order (every ecosystem found adds its
// commands to the same gate):
//
//	go.mod                                  go vet ./..., go test ./...
//	package.json                            "<pm> run <script>" for each of the
//	                                        scripts typecheck, lint, test it has
//	pytest.ini, pyproject.toml naming       pytest
//	pytest, setup.cfg with [tool:pytest]
//	Cargo.toml                              cargo test
//
// Detection only reads files, and a small bounded amount of them. It never
// runs anything: not the commands it names, not a package manager, not git.
// Nothing from a file's content reaches a command except which of the three
// script names exist, so a hostile repository cannot choose what is run.
// A file that cannot be read or understood adds nothing.
//
// It returns nil when nothing is detected. The gate has the default prio and
// remote, and Detected set.
func Detect(root string) *Repo {
	var run, sources []string
	add := func(cmds []string, source string) {
		if len(cmds) > 0 {
			run = append(run, cmds...)
			sources = append(sources, source)
		}
	}
	if isRegularFile(filepath.Join(root, "go.mod")) {
		add([]string{"go vet ./...", "go test ./..."}, "go.mod")
	}
	if cmds, pm := detectNodeScripts(root); len(cmds) > 0 {
		add(cmds, "package.json ("+pm+")")
	}
	if src := pytestSource(root); src != "" {
		add([]string{"pytest"}, src)
	}
	if isRegularFile(filepath.Join(root, "Cargo.toml")) {
		add([]string{"cargo test"}, "Cargo.toml")
	}
	if len(run) == 0 {
		return nil
	}
	return &Repo{Root: root, Gates: []Gate{{
		Name:     DetectedGateName,
		Run:      run,
		Prio:     DefaultPrio,
		Remote:   DefaultRemote,
		Detected: true,
		Sources:  sources,
	}}}
}

// isRegularFile reports whether path is a regular file (or a symlink to one).
func isRegularFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

// pytestSource names the file that says the repository uses pytest, or "".
// pytest.ini is enough on its own. pyproject.toml and setup.cfg configure much
// else too (ruff, mypy, packaging), and a pytest that collects nothing can
// never pass, so they count only when they speak of pytest: setup.cfg with a
// [tool:pytest] section, pyproject.toml with a [tool.pytest...] table or any
// other line naming pytest (a dev dependency). Only that yes or no is used.
func pytestSource(root string) string {
	if isRegularFile(filepath.Join(root, "pytest.ini")) {
		return "pytest.ini"
	}
	if anyLine(filepath.Join(root, "pyproject.toml"), func(l string) bool { return strings.Contains(l, "pytest") }) {
		return "pyproject.toml"
	}
	if anyLine(filepath.Join(root, "setup.cfg"), func(l string) bool { return l == "[tool:pytest]" }) {
		return "setup.cfg"
	}
	return ""
}

// anyLine reports whether a line of the bounded file at path, trimmed and not
// a "#" comment, satisfies match. An unreadable file has none.
func anyLine(path string, match func(string) bool) bool {
	data, err := readBounded(path)
	if err != nil {
		return false
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(nil, maxFileBytes)
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); !strings.HasPrefix(l, "#") && match(l) {
			return true
		}
	}
	return false
}

package repoconfig

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// nodeScripts are the package.json scripts a detected gate uses, in the order
// it runs them: cheapest and most specific first.
var nodeScripts = []string{"typecheck", "lint", "test"}

// lockfilePM maps a lockfile to the package manager that wrote it, in the
// order they are checked. No lockfile means npm.
var lockfilePM = []struct{ file, pm string }{
	{"pnpm-lock.yaml", "pnpm"},
	{"yarn.lock", "yarn"},
	{"bun.lockb", "bun"},
	{"bun.lock", "bun"},
}

// npmInitTestStub is the `test` script `npm init` writes. It always fails, so
// a gate that ran it could never be proven: it counts as "no test script".
const npmInitTestStub = "no test specified"

// detectNodeScripts returns "<pm> run <script>" for each of nodeScripts that
// root/package.json defines, and the package manager's name. It returns nil
// for a missing, oversized, unparsable or script-less package.json.
//
// Only the names of the scripts are used, never their bodies, so nothing in
// the file can reach a command line.
func detectNodeScripts(root string) (cmds []string, pm string) {
	data, err := readBounded(filepath.Join(root, "package.json"))
	if err != nil {
		return nil, ""
	}
	var pkg struct {
		Scripts map[string]json.RawMessage `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, ""
	}
	pm = packageManager(root)
	for _, name := range nodeScripts {
		var body string
		raw, ok := pkg.Scripts[name]
		if !ok || json.Unmarshal(raw, &body) != nil || strings.TrimSpace(body) == "" {
			continue // absent, not a string, or blank
		}
		if name == "test" && strings.Contains(body, npmInitTestStub) {
			continue
		}
		cmds = append(cmds, pm+" run "+name)
	}
	return cmds, pm
}

// packageManager picks the package manager from the lockfile in root.
func packageManager(root string) string {
	for _, l := range lockfilePM {
		if isRegularFile(filepath.Join(root, l.file)) {
			return l.pm
		}
	}
	return "npm"
}

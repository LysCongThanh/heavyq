package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/gitutil"
)

// printNestedWorkTreesHint is the `heavyq doctor` check for the repository
// around the current directory; it says nothing outside one. Informational:
// it never changes doctor's exit code.
func printNestedWorkTreesHint(w io.Writer) {
	wd, err := os.Getwd()
	if err != nil {
		return
	}
	root, err := gitutil.TopLevel(wd)
	if err != nil {
		return
	}
	if hint := nestedWorkTreesHint(root); hint != "" {
		fmt.Fprintln(w, hint)
	}
}

// nestedWorkTreesHint is the line `heavyq doctor` and `heavyq init` print when
// the repository holds agent work trees (.claude/worktrees) that the version
// control system does not ignore, or "" when there is nothing to say. Tree
// identity already leaves them out; status listings and editors do not, so the
// hint names the one command that hides them there too, in this clone only
// (info/exclude is not committed).
func nestedWorkTreesHint(root string) string {
	dirs := gitutil.UnignoredNestedDirs(root)
	if len(dirs) == 0 {
		return ""
	}
	var cmds []string
	for _, d := range dirs {
		cmds = append(cmds, fmt.Sprintf("echo '%s/' >> \"$(git rev-parse --git-path info/exclude)\"", d))
	}
	return fmt.Sprintf("nested work trees: %s is inside this repository and not ignored, so git status lists it (heavyq leaves it out of the tree identity either way); to hide it, run: %s",
		strings.Join(dirs, ", "), strings.Join(cmds, "; "))
}

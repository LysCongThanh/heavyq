package gitutil

import "strings"

// UncommittedFiles counts the files of the work tree at repo (a path from
// TopLevel) that differ from what a commit would hold: tracked files that are
// modified, staged, deleted or renamed, files with a merge conflict, and
// untracked files that are not ignored. It is the number of files a run that
// ships only HEAD leaves behind, and 0 exactly when IsClean is true.
//
// Untracked directories are listed file by file, so the count is of files, not
// of directories. A nested repository or linked work tree is another
// repository's: it does not count (see isNestedRepo), nor do the paths in
// NestedWorkTreeDirs. A changed submodule counts once.
func UncommittedFiles(repo string) (int, error) {
	args := append([]string{"status", "--porcelain", "-z", "--untracked-files=all", "--ignore-submodules=none"},
		untrackedSpecs(nil)...)
	out, err := gitRaw(repo, args...)
	if err != nil {
		return 0, err
	}
	fields := strings.Split(out, "\x00")
	n := 0
	for i := 0; i < len(fields); i++ {
		entry := fields[i] // "XY path"
		if len(entry) < 4 {
			continue
		}
		x, y, path := entry[0], entry[1], entry[3:]
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			i++ // a rename or copy is followed by its original path: one file, not two
		}
		if x == '?' && isNestedRepo(repo, path) {
			continue
		}
		n++
	}
	return n, nil
}

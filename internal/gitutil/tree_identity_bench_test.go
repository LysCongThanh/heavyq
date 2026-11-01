package gitutil

import (
	"fmt"
	"path/filepath"
	"testing"
)

// benchRepoFiles is the size of the generated repository: the target is an
// identity in under 50 ms for a repo of this size.
const benchRepoFiles = 10_000

// bigRepo generates a committed repository of benchRepoFiles small files in
// 100 directories.
func bigRepo(b *testing.B) string {
	b.Helper()
	dir := b.TempDir()
	gitIn(b, dir, "init", "-q", "-b", "main")
	for i := 0; i < benchRepoFiles; i++ {
		write(b, filepath.Join(dir, fmt.Sprintf("pkg%03d", i%100), fmt.Sprintf("file%05d.txt", i)),
			fmt.Sprintf("package p%d\n\nconst Value = %d // generated\n", i, i))
	}
	gitIn(b, dir, "add", ".")
	gitIn(b, dir, "commit", "-q", "-m", "10k files")
	return dir
}

// BenchmarkTreeIdentity measures WorkTree on a 10k-file repository, clean and
// dirty (a few edits and untracked files, the shape of an agent mid-task):
//
//	go test ./internal/gitutil -run '^$' -bench TreeIdentity -benchtime 20x
func BenchmarkTreeIdentity(b *testing.B) {
	if testing.Short() {
		b.Skip("generates a 10k-file repository")
	}
	dir := bigRepo(b)
	run := func(b *testing.B, f func(string) error) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := f(dir); err != nil {
				b.Fatal(err)
			}
		}
	}
	workTree := func(d string) error { _, err := WorkTree(d); return err }
	identify := func(d string) error { _, err := Identify(d); return err }

	b.Run("clean", func(b *testing.B) { run(b, workTree) })
	b.Run("clean-identify", func(b *testing.B) { run(b, identify) })
	for i := 0; i < 5; i++ {
		write(b, filepath.Join(dir, "pkg000", fmt.Sprintf("file%05d.txt", i*100)), "edited\n")
		write(b, filepath.Join(dir, "scratch", fmt.Sprintf("note%d.txt", i)), "untracked\n")
	}
	b.Run("dirty", func(b *testing.B) { run(b, workTree) })
	b.Run("dirty-identify", func(b *testing.B) { run(b, identify) })
}

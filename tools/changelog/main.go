// Command changelog folds changelog.d/ fragments into CHANGELOG.md and checks pull requests for one.
//
// Every pull request that changes behaviour adds changelog.d/<slug>.<kind>.md (kind: added, changed, fixed,
// security) instead of editing CHANGELOG.md, so parallel pull requests never conflict on it. At release time:
//
//	go run ./tools/changelog release 0.5.0 [-date 2026-11-02] [-dry-run]
//
// moves the fragments, and whatever [Unreleased] already holds, into "## [0.5.0] - <date>" and deletes them.
// CI runs the other subcommand with the files a pull request changed on stdin:
//
//	go run ./tools/changelog check -labels "ci,no-changelog" < changed-files.txt
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: changelog release <version> [-date YYYY-MM-DD] [-dry-run] | changelog check [-labels a,b] < files")
		return 2
	}
	switch args[0] {
	case "release":
		return release(args[1:], stdout, stderr)
	case "check":
		return check(args[1:], stdin, stdout, stderr)
	}
	fmt.Fprintf(stderr, "changelog: unknown command %q (release or check)\n", args[0])
	return 2
}

func release(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("release", flag.ContinueOnError)
	fs.SetOutput(stderr)
	date := fs.String("date", time.Now().UTC().Format("2006-01-02"), "release date")
	dir := fs.String("dir", fragmentDir, "fragment directory")
	file := fs.String("file", "CHANGELOG.md", "changelog file")
	dry := fs.Bool("dry-run", false, "print the new changelog, change nothing")
	// the version comes first: `release 0.5.0 -dry-run`
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(stderr, "usage: changelog release <version> [-date YYYY-MM-DD] [-dir DIR] [-file FILE] [-dry-run]")
		return 2
	}
	version := strings.TrimPrefix(args[0], "v")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return 2
	}
	if _, err := time.Parse("2006-01-02", *date); err != nil {
		fmt.Fprintf(stderr, "changelog: -date must be YYYY-MM-DD: %v\n", err)
		return 2
	}
	if err := foldFiles(version, *date, *dir, *file, *dry, stdout); err != nil {
		fmt.Fprintln(stderr, "changelog:", err)
		return 1
	}
	return 0
}

// foldFiles reads the fragments and the changelog, writes the folded changelog and only then deletes the
// fragments, so a failure leaves them where they were.
func foldFiles(version, date, dir, file string, dry bool, stdout io.Writer) error {
	frags, err := readFragments(dir)
	if err != nil {
		return err
	}
	old, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	folded, err := fold(string(old), version, date, frags)
	if err != nil {
		return err
	}
	if dry {
		_, err = io.WriteString(stdout, folded)
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, []byte(folded), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, file); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	for _, f := range frags {
		if err := os.Remove(f.Path); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "changelog: %d fragment(s) folded into %s as %s\n", len(frags), file, version)
	return nil
}

func check(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	labels := fs.String("labels", "", "comma-separated labels of the pull request")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	b, err := io.ReadAll(io.LimitReader(stdin, 4<<20))
	if err != nil {
		fmt.Fprintln(stderr, "changelog:", err)
		return 2
	}
	problems := checkPullRequest(strings.Split(string(b), "\n"), strings.Split(*labels, ","))
	for _, p := range problems {
		fmt.Fprintln(stderr, "changelog:", p)
	}
	if len(problems) > 0 {
		return 1
	}
	fmt.Fprintln(stdout, "changelog: ok")
	return 0
}

package gitutil

import (
	"strconv"
	"strings"
	"time"
)

// CommitFiles is one commit with the files it changed. It is what churn
// detection (internal/stance) reads: who wrote what is not needed.
type CommitFiles struct {
	Hash    string
	Subject string
	At      time.Time
	Files   []string // repository-relative, as git prints them
}

// RecentCommits returns the newest commits reachable from HEAD of the work
// tree around dir that were made at or after since, newest first, at most max
// of them. Merge commits are left out (they carry no file list of their own).
// A repository without commits has none, which is not an error.
func RecentCommits(dir string, since time.Time, max int) ([]CommitFiles, error) {
	if max <= 0 {
		return nil, nil
	}
	if _, err := git(dir, "rev-parse", "--verify", "-q", "HEAD"); err != nil {
		return nil, nil
	}
	out, err := gitRaw(dir, "-c", "core.quotepath=off", "log", "--no-merges", "-n", strconv.Itoa(max),
		"--name-only", "--format=%x01%H%x1f%ct%x1f%s")
	if err != nil {
		return nil, err
	}
	var commits []CommitFiles
	for _, block := range strings.Split(out, "\x01") {
		if strings.TrimSpace(block) == "" {
			continue
		}
		head, files, _ := strings.Cut(block, "\n")
		parts := strings.SplitN(head, "\x1f", 3)
		if len(parts) != 3 {
			continue
		}
		secs, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			continue
		}
		c := CommitFiles{Hash: parts[0], Subject: parts[2], At: time.Unix(secs, 0)}
		if c.At.Before(since) {
			break // newest first: everything after is older
		}
		for _, f := range strings.Split(files, "\n") {
			if f = strings.TrimSpace(f); f != "" {
				c.Files = append(c.Files, f)
			}
		}
		commits = append(commits, c)
	}
	return commits, nil
}

package collect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Limits of the gh collector.
const (
	// GHInterval is the least time between two calls to gh: the answer is cached
	// under the git common dir and served from there until it is this old. A
	// failed call counts too, so an unauthenticated or offline gh is tried once
	// per interval, not once per refresh.
	GHInterval = 5 * time.Minute
	// ghTimeout bounds one collection (both calls). The flow refresh of a hook
	// has a 4 s budget: a gh that is slower is given up on, and tried again
	// after GHInterval.
	ghTimeout = 3 * time.Second
	// maxGHOutput bounds what is read from gh.
	maxGHOutput = 4 << 20
)

// GHRunner runs `gh args...` in dir and returns its standard output.
type GHRunner func(ctx context.Context, dir string, args ...string) ([]byte, error)

// ghProgram is the binary the real runner runs.
const ghProgram = "gh"

// execGH is the real runner. It never prompts, never checks for updates and
// never inherits a terminal: a background refresh must not hang on gh.
func execGH(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, ghProgram, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "GH_SPINNER_DISABLED=1", "NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &limitWriter{w: &stdout, n: maxGHOutput}, &stderr
	if err := cmd.Run(); err != nil {
		msg, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("gh %s: %s", strings.Join(args[:min(len(args), 2)], " "), msg)
	}
	return stdout.Bytes(), nil
}

// limitWriter fails once more than n bytes were written.
type limitWriter struct {
	w *bytes.Buffer
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.w.Len()+len(p) > l.n {
		return 0, errors.New("output too large")
	}
	return l.w.Write(p)
}

// ghPR is a pull request as `gh pr list --json` prints it. gh prints a null
// time as 0001-01-01, which decodes to the zero time.
type ghPR struct {
	Number      int       `json:"number"`
	HeadRefName string    `json:"headRefName"`
	State       string    `json:"state"` // OPEN, MERGED, CLOSED
	IsDraft     bool      `json:"isDraft"`
	CreatedAt   time.Time `json:"createdAt"`
	MergedAt    time.Time `json:"mergedAt"`
	ClosedAt    time.Time `json:"closedAt"`
}

// ghRun is a workflow run as `gh run list --json` prints it.
type ghRun struct {
	HeadBranch string    `json:"headBranch"`
	HeadSha    string    `json:"headSha"`
	Event      string    `json:"event"`
	Status     string    `json:"status"`     // queued, in_progress, completed
	Conclusion string    `json:"conclusion"` // success, failure, cancelled, skipped, ... ("" until completed)
	CreatedAt  time.Time `json:"createdAt"`
	StartedAt  time.Time `json:"startedAt"` // of the latest attempt
	UpdatedAt  time.Time `json:"updatedAt"`
}

// ghSnapshot is what the cache file holds: the last answer of gh, and when gh
// was last asked.
type ghSnapshot struct {
	AttemptedAt time.Time `json:"attempted_at"` // the last call, successful or not
	FetchedAt   time.Time `json:"fetched_at"`   // the last successful call; zero if none was
	Error       string    `json:"error,omitempty"`
	PRs         []ghPR    `json:"prs"`
	Runs        []ghRun   `json:"runs"`
}

func ghCachePath(common string) string {
	return filepath.Join(common, "heavyq", "flow", "gh-cache.json")
}

func readGHCache(common string) *ghSnapshot {
	if common == "" {
		return nil
	}
	b, err := os.ReadFile(ghCachePath(common))
	if err != nil || len(b) > maxGHOutput*2 {
		return nil
	}
	var s ghSnapshot
	if json.Unmarshal(b, &s) != nil {
		return nil // damaged: asked for again
	}
	return &s
}

// writeGHCache stores s atomically; a failure only costs the next call.
func writeGHCache(common string, s *ghSnapshot) {
	if common == "" {
		return
	}
	path := ghCachePath(common)
	b, err := json.Marshal(s)
	if err != nil || os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gh-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(b)
	if errors.Join(werr, tmp.Close()) != nil || os.Rename(tmp.Name(), path) != nil {
		_ = os.Remove(tmp.Name())
	}
}

// snapshotGH is the answer of gh as of now: the cache while it is younger than
// GHInterval, else a new call. When the call fails the last good answer stays
// (it is returned, and the failure is remembered so gh is not asked again for an
// interval); with none, the snapshot is nil. The error is for the log.
func snapshotGH(o Options, run GHRunner, repo string) (*ghSnapshot, error) {
	now := o.now()
	cached := readGHCache(o.Common)
	if cached != nil {
		if age := now.Sub(cached.AttemptedAt); age >= 0 && age < GHInterval {
			return usable(cached), nil
		}
	}
	snap := &ghSnapshot{AttemptedAt: now, FetchedAt: now}
	err := fetchGH(run, repo, snap)
	if err != nil {
		snap = &ghSnapshot{AttemptedAt: now, Error: err.Error()}
		if cached != nil {
			snap.FetchedAt, snap.PRs, snap.Runs = cached.FetchedAt, cached.PRs, cached.Runs
		}
	}
	writeGHCache(o.Common, snap)
	return usable(snap), err
}

// usable is nil for a snapshot that never fetched anything.
func usable(s *ghSnapshot) *ghSnapshot {
	if s == nil || s.FetchedAt.IsZero() {
		return nil
	}
	return s
}

// fetchGH asks for the pull requests and the workflow runs of the repository
// around repo, together within ghTimeout.
func fetchGH(run GHRunner, repo string, into *ghSnapshot) error {
	ctx, cancel := context.WithTimeout(context.Background(), ghTimeout)
	defer cancel()
	out, err := run(ctx, repo, "pr", "list", "--state", "all", "--limit", "100",
		"--json", "number,headRefName,state,isDraft,createdAt,mergedAt,closedAt")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, &into.PRs); err != nil {
		return fmt.Errorf("gh pr list: %w", err)
	}
	out, err = run(ctx, repo, "run", "list", "--limit", "200",
		"--json", "headBranch,headSha,event,status,conclusion,createdAt,startedAt,updatedAt")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, &into.Runs); err != nil {
		return fmt.Errorf("gh run list: %w", err)
	}
	return nil
}

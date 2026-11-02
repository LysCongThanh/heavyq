// Package runspan records a run span: the plan of one long, usually unattended
// stretch of agent work (a harness turn, a workflow, a night). `heavyq
// preflight` opens one, `heavyq run end` closes it, and later readers (recap,
// the flow state, the brief) compare the plan with what happened.
//
// A span is one small JSON file, <git common dir>/heavyq/runs/<id>.json, shared
// by every work tree of the repository and never committed. Files are written
// atomically (temp file and rename, mode 0600), so a reader never sees half a
// span, and a damaged file is skipped, never fatal. Readers decide what "open"
// means with Current, so a span nobody closed does not shadow the next run.
package runspan

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Limits on what a span file may hold; anything bigger is damaged or hostile.
const (
	maxSpanBytes = 64 << 10
	maxListed    = 500 // spans List returns, newest first
	// lapseGrace is how long past its expected end a span still counts as current.
	lapseGrace = time.Hour
	// lapseWithoutEnd is how long a span with no expected end counts as current.
	lapseWithoutEnd = 24 * time.Hour
)

// Span is the plan of one run. The JSON is the file format: the first seven
// keys are fixed by the design, Ended is added when the span is closed.
type Span struct {
	ID          string     `json:"id"`
	Started     time.Time  `json:"started"`
	ExpectedEnd *time.Time `json:"expected_end"` // null when the run has no stated length
	Goal        string     `json:"goal"`
	Items       []string   `json:"items"`
	Parallel    int        `json:"parallel"`
	Host        string     `json:"host"`
	Ended       *time.Time `json:"ended,omitempty"`
}

// Open reports whether the span has not been ended. A span that was never
// ended and is long past its expected end is still Open here; Current is the
// "is a run going on" question.
func (s Span) Open() bool { return s.Ended == nil }

// Lapsed reports whether an open span is so far past its expected end (or, with
// none, so old) that nobody is running it any more.
func (s Span) Lapsed(now time.Time) bool {
	if s.ExpectedEnd != nil {
		return now.After(s.ExpectedEnd.Add(lapseGrace))
	}
	return now.After(s.Started.Add(lapseWithoutEnd))
}

// Errors of the package; use errors.Is.
var (
	ErrNotFound = errors.New("no such run span")
	ErrNoneOpen = errors.New("no open run span")
	ErrBadID    = errors.New("bad run span id (letters, digits, . _ -; at most 64)")
	ErrEnded    = errors.New("run span already ended")
	ErrExists   = errors.New("run span already exists")
)

var idRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidID reports whether id can name a span (it becomes a file name).
func ValidID(id string) bool { return idRe.MatchString(id) }

// Dir is where the spans of the repository with git common dir common live.
func Dir(common string) string { return filepath.Join(common, "heavyq", "runs") }

func spanPath(common, id string) string { return filepath.Join(Dir(common), id+".json") }

// NewID is a fresh id: the UTC start time and 4 random hex digits, so ids sort
// by time and two spans opened in the same second differ.
func NewID(now time.Time) string {
	var b [2]byte
	_, _ = rand.Read(b[:])
	return now.UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b[:])
}

// Open writes s as a new span and returns it. A zero ID gets a fresh one, a zero
// Started becomes now. Any span still open is ended first: a repository has one
// run at a time, and an old one must not shadow the new.
func Open(common string, s Span, now time.Time) (Span, error) {
	if s.ID == "" {
		s.ID = NewID(now)
	}
	if !ValidID(s.ID) {
		return Span{}, fmt.Errorf("%w: %q", ErrBadID, s.ID)
	}
	if s.Started.IsZero() {
		s.Started = now
	}
	if s.Items == nil {
		s.Items = []string{}
	}
	s.Ended = nil
	if _, err := os.Stat(spanPath(common, s.ID)); err == nil {
		return Span{}, fmt.Errorf("%w: %s", ErrExists, s.ID)
	}
	all, err := List(common)
	if err != nil {
		return Span{}, err
	}
	for _, o := range all {
		if o.Open() {
			if _, err := End(common, o.ID, now); err != nil && !errors.Is(err, ErrEnded) {
				return Span{}, err
			}
		}
	}
	if err := write(common, s); err != nil {
		return Span{}, err
	}
	return s, nil
}

// End closes the span id, or with id "" the current one. Ending a span that is
// already ended returns it with ErrEnded.
func End(common, id string, now time.Time) (Span, error) {
	if id == "" {
		cur, err := Current(common, now)
		if err != nil {
			return Span{}, err
		}
		if cur == nil {
			return Span{}, ErrNoneOpen
		}
		id = cur.ID
	}
	s, err := Get(common, id)
	if err != nil {
		return Span{}, err
	}
	if s.Ended != nil {
		return s, ErrEnded
	}
	t := now
	s.Ended = &t
	return s, write(common, s)
}

// Get reads one span.
func Get(common, id string) (Span, error) {
	if !ValidID(id) {
		return Span{}, fmt.Errorf("%w: %q", ErrBadID, id)
	}
	s, err := read(spanPath(common, id))
	if errors.Is(err, fs.ErrNotExist) {
		return Span{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return s, err
}

// Current is the newest span that is open and not lapsed, or nil when none is.
func Current(common string, now time.Time) (*Span, error) {
	all, err := List(common)
	if err != nil {
		return nil, err
	}
	for _, s := range all {
		if s.Open() && !s.Lapsed(now) {
			return &s, nil
		}
	}
	return nil, nil
}

// List returns the spans, newest first (at most 500). Damaged files and files
// whose name is not their id are skipped.
func List(common string) ([]Span, error) {
	entries, err := os.ReadDir(Dir(common))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Span
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || e.IsDir() || !ValidID(id) {
			continue
		}
		if s, err := read(filepath.Join(Dir(common), e.Name())); err == nil && s.ID == id {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Started.Equal(out[j].Started) {
			return out[i].Started.After(out[j].Started)
		}
		return out[i].ID > out[j].ID
	})
	if len(out) > maxListed {
		out = out[:maxListed]
	}
	return out, nil
}

// Package collect holds the optional collectors of internal/flow: the sources
// that need git or gh and so can never be part of the pure derivation. A
// collector returns spans for a work item (flow.External); flow paints them
// into the item's timeline like any other.
//
//   - Git: conflict_rework, from merge commits that merged the default branch
//     into an item's branch and conflicted. Local git only; works offline.
//   - GH: ci_wait (workflow runs of the item's branch still going) and
//     merge_wait (checks green, pull request not merged), from gh when it is
//     installed and logged in; at most one call per five minutes.
//
// Collectors fail open: whatever cannot be read drops its states, never the
// flow state. A returned error comes with the spans that could be built, and is
// for the log only.
package collect

import (
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/LysCongThanh/heavyq/internal/flow"
)

// Options say what to collect for.
type Options struct {
	// Items are the ids of the anchor's work items. A branch or pull request
	// belongs to the item whose id its name carries (ItemForBranch); with no
	// items there is nothing to attribute spans to, and nothing is collected.
	Items []string
	// Common is the git common dir: the gh cache lives under it. Empty: no cache.
	Common string
	// GH runs the gh binary; nil runs the real one. Tests inject a fake.
	GH GHRunner
	// NoGH turns the gh collector off (offline work, or a repository that is
	// not on GitHub).
	NoGH bool
	// Now is the clock of the gh cache; nil is time.Now.
	Now func() time.Time
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// All is the git and the gh collector in one. Each runs even when the other
// fails; the errors, if any, are joined.
func All(o Options) flow.Collector {
	cs := []flow.Collector{Git(o)}
	if !o.NoGH {
		cs = append(cs, GH(o))
	}
	return func(repo string, from, to time.Time) ([]flow.External, error) {
		var out []flow.External
		var errs []error
		for _, c := range cs {
			ext, err := c(repo, from, to)
			out = append(out, ext...)
			errs = append(errs, err)
		}
		return out, errors.Join(errs...)
	}
}

// ItemForBranch names the item a branch (or pull request head) belongs to: the
// item whose id is the branch's last path segment or a dash-delimited part of
// it ("feat/flow-collectors" and "fix/flow-collectors-2" are flow-collectors).
// The longest id wins, so "flow" does not steal "flow-collectors". "" when no id
// matches.
func ItemForBranch(branch string, ids []string) string {
	seg := strings.ToLower(branch[strings.LastIndex(branch, "/")+1:])
	best := ""
	for _, id := range ids {
		lid := strings.ToLower(id)
		if lid != "" && len(id) > len(best) && containsWord(seg, lid) {
			best = id
		}
	}
	return best
}

// containsWord: word occurs in s, not inside a longer run of letters and digits.
func containsWord(s, word string) bool {
	for from := 0; ; {
		i := strings.Index(s[from:], word)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(word)
		if (i == 0 || !isWordRune(rune(s[i-1]))) && (end == len(s) || !isWordRune(rune(s[end]))) {
			return true
		}
		from = i + 1
	}
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

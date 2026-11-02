package stance

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DecisionDir is where decision records live, relative to the work tree root:
// .heavyq/decisions/<item>.md. The directory is bookkeeping: it is left out of
// the tree identity (gitutil), so writing a record voids no proof.
const DecisionDir = ".heavyq/decisions"

// maxDecisionBytes bounds the record that is read.
const maxDecisionBytes = 256 << 10

// DecisionPath is the record of key under the work tree root top. key is an
// item id (it may contain "/", which makes subdirectories) or DecisionKey.
func DecisionPath(top, key string) string {
	return filepath.Join(top, filepath.FromSlash(DecisionDir), filepath.FromSlash(key)+".md")
}

var keyCleanRe = regexp.MustCompile(`[^a-z0-9._-]+`)

// DecisionKey names the record of a lane that holds no item: the lane's name
// reduced to [a-z0-9._-].
func DecisionKey(item, lane string) string {
	if item != "" {
		return item
	}
	k := strings.Trim(keyCleanRe.ReplaceAllString(strings.ToLower(lane), "-"), ".-_")
	if k == "" {
		return "lane"
	}
	return k
}

// ReadDecision returns the record at path. A missing file is (nil, false, nil).
func ReadDecision(path string) (data []byte, exists bool, err error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	if !fi.Mode().IsRegular() || fi.Size() > maxDecisionBytes {
		return nil, true, fmt.Errorf("%s is not a regular file of at most %d bytes", path, maxDecisionBytes)
	}
	data, err = io.ReadAll(io.LimitReader(f, maxDecisionBytes))
	return data, true, err
}

// section is one heading of a markdown document with the lines under it, up
// to the next heading of the same or a higher level (sub-headings are part of
// their parent).
type section struct {
	title string
	lines []string
}

var headingRe = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*#*\s*$`)

func sections(md string) []section {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	var out []section
	for i, ln := range lines {
		m := headingRe.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		s := section{title: strings.ToLower(m[2])}
		for _, next := range lines[i+1:] {
			if n := headingRe.FindStringSubmatch(next); n != nil && len(n[1]) <= len(m[1]) {
				break
			}
			s.lines = append(s.lines, next)
		}
		out = append(out, s)
	}
	return out
}

// findSection returns the first section whose title contains any of words.
func findSection(secs []section, words ...string) (section, bool) {
	for _, s := range secs {
		for _, w := range words {
			if strings.Contains(s.title, w) {
				return s, true
			}
		}
	}
	return section{}, false
}

var htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)

// hasText: the section says something beyond blank lines and comments.
func (s section) hasText() bool {
	return strings.TrimSpace(htmlCommentRe.ReplaceAllString(strings.Join(s.lines, "\n"), "")) != ""
}

// tableRows returns the data rows of the markdown tables in s (header and
// separator rows left out), lower-cased.
func (s section) tableRows() []string {
	var rows []string
	inTable := false
	for _, ln := range s.lines {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, "|") {
			inTable = false
			continue
		}
		if !inTable { // the first row of a table is its header
			inTable = true
			continue
		}
		if strings.Trim(t, "|-: \t") == "" { // the |---|---| separator
			continue
		}
		rows = append(rows, strings.ToLower(t))
	}
	return rows
}

// DecisionProblems checks the structure of a decision record and returns what
// is missing, one line each; none means the structure is complete. It never
// judges the content: whether the thinking is any good is for the human.
//
// A record needs these sections (markdown headings, matched on a word of the
// title, any level, any case), each with some text:
//   - "clean-slate answer" (title has "clean-slate" or "clean slate"),
//   - "options": a table with at least 3 rows, one that names the status quo
//     and a different one marked radical (the word, or a star),
//   - "constraints": each constraint classified, the section uses both the
//     words "real" and "inertia",
//   - "choice" (or "chosen") and "migration"; one heading may carry both words.
func DecisionProblems(md string) []string {
	secs := sections(md)
	var p []string
	need := func(label string, words ...string) (section, bool) {
		s, ok := findSection(secs, words...)
		switch {
		case !ok:
			p = append(p, fmt.Sprintf("missing section: %s (a heading with %q)", label, words[0]))
		case !s.hasText():
			p = append(p, fmt.Sprintf("section is empty: %s", label))
		default:
			return s, true
		}
		return s, false
	}
	need("clean-slate answer", "clean-slate", "clean slate")
	if s, ok := need("options", "option"); ok {
		p = append(p, optionProblems(s.tableRows())...)
	}
	if s, ok := need("constraints", "constraint"); ok {
		body := strings.ToLower(strings.Join(s.lines, "\n"))
		if !strings.Contains(body, "real") || !strings.Contains(body, "inertia") {
			p = append(p, "constraints must be classified: use both the words REAL and INERTIA")
		}
	}
	need("choice", "choice", "chosen")
	need("migration", "migration")
	return p
}

func optionProblems(rows []string) []string {
	var p []string
	if len(rows) < 3 {
		p = append(p, fmt.Sprintf("options: a table with at least 3 options, found %d", len(rows)))
	}
	quo, radical := -1, -1
	for i, r := range rows {
		if strings.Contains(r, "status quo") {
			quo = i
			break
		}
	}
	for i, r := range rows {
		if i != quo && (strings.Contains(r, "radical") || strings.Contains(r, "★")) {
			radical = i
			break
		}
	}
	if quo < 0 {
		p = append(p, `options: no row names the status quo ("status quo")`)
	}
	if radical < 0 {
		p = append(p, `options: no radical option, a row marked "radical" or ★ (not the status quo)`)
	}
	return p
}

// Package anchor reads the durable "what is left" list of a repository: the
// goal and the checklist items in .heavyq/anchor.md, plus read-only
// checklists found in other markdown files (plan phases, docs).
//
// Parsing is lenient (unknown lines are ignored, odd markdown never fails) and
// every oddity is reported as a Warning instead of an error. Rewriting is
// strict: SetChecked flips one checkbox character and nothing else.
package anchor

import (
	"errors"
	"fmt"
)

// Limits that keep a hostile or accidental huge checklist cheap to load.
const (
	MaxItems     = 2000    // items kept per Load (and per Parse call)
	MaxFileBytes = 1 << 20 // 1 MiB per markdown file
	MaxIDLen     = 64      // longest anchor.md item id
	// DefaultFile is the anchor file when heavyq.toml [anchor] file is unset.
	DefaultFile = ".heavyq/anchor.md"
)

// Proof kinds. The empty kind means the item declares no proof.
const (
	ProofCmd  = "cmd"  // a shell command that must succeed on the current tree
	ProofGate = "gate" // the name of a gate in heavyq.toml
)

// File is a parsed anchor file merged, by Load, with the extra sources.
type File struct {
	Path     string    `json:"path"`      // repo-relative, slash separated
	Goal     string    `json:"goal"`      // text after "# Goal:"; "" when missing
	GoalLine int       `json:"goal_line"` // 1-based; 0 when missing
	Items    []Item    `json:"items"`
	Warnings []Warning `json:"warnings"` // never nil, so JSON shows []
}

// Item is one checklist line.
type Item struct {
	ID       string `json:"id"`
	Text     string `json:"text"` // without the id prefix and the proof/needs spans
	Checked  bool   `json:"checked"`
	Proof    Proof  `json:"proof"`
	Needs    string `json:"needs,omitempty"`
	Source   string `json:"source"`    // file the item was read from
	Line     int    `json:"line"`      // 1-based
	ReadOnly bool   `json:"read_only"` // true for extra sources: heavyq never edits them
}

// Proof says how an item is proven.
type Proof struct {
	Kind  string `json:"kind,omitempty"`  // ProofCmd, ProofGate or ""
	Value string `json:"value,omitempty"` // command line or gate name
}

// Warning is something the parser tolerated but the author should know about.
type Warning struct {
	Source string `json:"source"`
	Line   int    `json:"line"` // 0 when the warning is about the whole file
	Msg    string `json:"msg"`
}

func (w Warning) String() string {
	if w.Line == 0 {
		return fmt.Sprintf("%s: %s", w.Source, w.Msg)
	}
	return fmt.Sprintf("%s:%d: %s", w.Source, w.Line, w.Msg)
}

// Errors returned by SetChecked and SetItemChecked.
var (
	ErrNotFound   = errors.New("anchor: item not found")
	ErrReadOnly   = errors.New("anchor: item comes from a read-only source")
	ErrConcurrent = errors.New("anchor: file changed while rewriting; retry")
	ErrTooLarge   = errors.New("anchor: file is larger than 1 MiB")
	ErrNotRegular = errors.New("anchor: not a regular file (symlinks are refused)")
)

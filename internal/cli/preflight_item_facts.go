package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/anchor"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/itemstatus"
	"github.com/LysCongThanh/heavyq/internal/preflight"
	"github.com/LysCongThanh/heavyq/internal/proof"
	"github.com/LysCongThanh/heavyq/internal/redact"
)

// itemFacts turns the derived item rows into what the preflight checks read:
// the proof as written, why it cannot run (a missing gate or program), its
// typical duration from the ledger, and the `after:` ids of the anchor line.
// It also returns every shell command the proofs of open items stand for, for
// the permission check.
func itemFacts(s *itemSet, rows []itemRow, recs []history.Record) (facts []preflight.ItemFact, commands []string) {
	durations := preflight.P50ByCmdHash(recs)
	seen := map[string]bool{}
	lines := newSourceLines(s.root)
	for _, r := range rows {
		it, st := r.Item, r.Status
		f := preflight.ItemFact{ID: it.ID, Text: it.Text, Where: fmt.Sprintf("%s:%d", it.Source, it.Line),
			State: st.State, Done: st.Done, After: preflight.ParseAfter(lines.at(it.Source, it.Line))}
		if st.State == itemstatus.Blocked {
			f.Needs, f.NeedsInFile = st.Needs, it.Needs != ""
			if f.Needs == "" {
				f.Needs = "(no reason recorded)"
			}
		}
		if it.Proof.Kind != "" {
			var cmds []string
			_, cmds, f.ProofProblem = proofCommands(s, it.Proof)
			f.Proof = redact.Command(proofLabel(it.Proof))
			var hashes []string
			for _, c := range cmds {
				hashes = append(hashes, history.CmdHash(proof.Argv(c)))
				if f.ProofProblem == "" {
					f.ProofProblem = missingProgram(c, s.root)
				}
			}
			if f.Open() && f.ProofProblem == "" { // a proof that cannot run is already a finding
				for _, c := range cmds {
					if !seen[c] {
						seen[c] = true
						commands = append(commands, c)
					}
				}
			}
			f.Est = durations.Estimate(hashes)
		}
		facts = append(facts, f)
	}
	return facts, commands
}

// sourceLines reads the lines of the markdown files items came from, each once.
type sourceLines struct {
	root  string
	files map[string][]string
}

func newSourceLines(root string) *sourceLines {
	return &sourceLines{root: root, files: map[string][]string{}}
}

// at is line n (1-based) of the repository-relative file name, or "" when it
// cannot be read. Item sources are inside the repository by construction.
func (l *sourceLines) at(name string, n int) string {
	ls, ok := l.files[name]
	if !ok {
		if b, err := os.ReadFile(filepath.Join(l.root, filepath.FromSlash(name))); err == nil && len(b) <= anchorMaxBytes {
			ls = strings.Split(string(b), "\n")
		}
		l.files[name] = ls
	}
	if n < 1 || n > len(ls) {
		return ""
	}
	return ls[n-1]
}

// anchorMaxBytes mirrors the anchor parser's per-file limit.
const anchorMaxBytes = anchor.MaxFileBytes

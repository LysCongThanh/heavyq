package anchor

import (
	"regexp"
	"strings"
)

// idPrefix matches an item id at the start of the text, with optional
// emphasis or code around it: "id: x", "**id**: x", "**id:** x", "_id_: x",
// "`id`: x". Groups: 1 opener, 2 id, 3 closer before the colon, 4 closer after.
// A space (or the end of the text) must follow so "http://x" is not an id.
var idPrefix = regexp.MustCompile("^([*_`]{0,3})([a-z0-9][a-z0-9._-]*)([*_`]{0,3}):([*_`]{0,3})(?:[ \t]+(.*))?$")

// gateName mirrors the gate name rule of internal/repoconfig.
var gateName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// splitID cuts a valid "id:" prefix off text. ok is false when text has no
// valid prefix (then rest is text, unchanged).
func splitID(text string) (id, rest string, ok bool) {
	m := idPrefix.FindStringSubmatch(text)
	if m == nil {
		return "", text, false
	}
	open, id, closeA := m[1], m[2], m[3]
	// "_id_:" - the id pattern swallows the closing underscores; give them back.
	if closeA == "" && open != "" {
		for len(open) > 0 && strings.HasSuffix(id, "_") && open[0] == '_' && len(id) > 1 {
			id = id[:len(id)-1]
			open = open[1:]
		}
	}
	if len(id) > MaxIDLen {
		return "", text, false
	}
	return id, m[5], true
}

// itemMeta is what the inline `proof:` and `needs:` spans of a line say.
type itemMeta struct {
	text  string // line text without those spans, whitespace collapsed
	proof Proof
	needs string
	msgs  []string // problems to report as warnings
}

// extractMeta pulls the `proof: ...` and `needs: ...` code spans out of raw.
// Other code spans stay in the text. The first of each kind wins.
func extractMeta(raw string) itemMeta {
	var m itemMeta
	var rest strings.Builder
	last := 0
	for _, sp := range codeSpans(raw) {
		key, val, ok := metaKey(sp.content)
		if !ok {
			continue
		}
		rest.WriteString(raw[last:sp.start])
		rest.WriteByte(' ')
		last = sp.end
		switch key {
		case "proof":
			m.setProof(val)
		case "needs":
			if m.needs != "" {
				m.msgs = append(m.msgs, "more than one `needs:`; the first one is used")
			} else if val == "" {
				m.msgs = append(m.msgs, "empty `needs:` ignored")
			} else {
				m.needs = val
			}
		}
	}
	rest.WriteString(raw[last:])
	m.text = strings.Join(strings.Fields(rest.String()), " ")
	return m
}

// metaKey recognises "proof: v" / "needs: v" (key case-insensitive).
func metaKey(content string) (key, val string, ok bool) {
	for _, k := range []string{"proof", "needs"} {
		if len(content) > len(k) && strings.EqualFold(content[:len(k)], k) && content[len(k)] == ':' {
			return k, strings.TrimSpace(content[len(k)+1:]), true
		}
	}
	return "", "", false
}

func (m *itemMeta) setProof(val string) {
	switch {
	case m.proof.Kind != "":
		m.msgs = append(m.msgs, "more than one `proof:`; the first one is used")
	case val == "":
		m.msgs = append(m.msgs, "empty `proof:` ignored")
	case len(val) >= 5 && strings.EqualFold(val[:5], "gate:"):
		name := strings.TrimSpace(val[5:])
		if !gateName.MatchString(name) {
			m.msgs = append(m.msgs, "proof gate name "+quote(name)+" is invalid; proof ignored")
			return
		}
		m.proof = Proof{Kind: ProofGate, Value: name}
	default:
		m.proof = Proof{Kind: ProofCmd, Value: val}
	}
}

func quote(s string) string { return "\"" + s + "\"" }

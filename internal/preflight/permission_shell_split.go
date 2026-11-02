package preflight

import "strings"

// splitCommand cuts a shell command into its simple commands at && || ; | & and
// newlines outside quotes, and drops redirections and grouping. simple is false
// when the command substitutes ($( ) or backticks) or does not parse, since no
// rule can then be matched against its parts.
func splitCommand(s string) (segs []string, simple bool) {
	var cur strings.Builder
	var quote rune
	flush := func() {
		if seg := cleanSegment(cur.String()); seg != "" {
			segs = append(segs, seg)
		}
		cur.Reset()
	}
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch {
		case quote == '\'':
			cur.WriteRune(c)
			if c == '\'' {
				quote = 0
			}
		case quote == '"':
			if c == '`' || c == '$' && i+1 < len(rs) && rs[i+1] == '(' {
				return nil, false
			}
			cur.WriteRune(c)
			if c == '\\' && i+1 < len(rs) {
				i++
				cur.WriteRune(rs[i])
			} else if c == '"' {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
			cur.WriteRune(c)
		case c == '\\' && i+1 < len(rs):
			cur.WriteRune(c)
			i++
			cur.WriteRune(rs[i])
		case c == '`' || c == '$' && i+1 < len(rs) && rs[i+1] == '(':
			return nil, false
		case c == '&' && i > 0 && rs[i-1] == '>', c == '>' && i+1 < len(rs) && rs[i+1] == '&':
			cur.WriteRune(c) // 2>&1 is a redirection, not an operator
		case c == ';' || c == '|' || c == '&' || c == '\n':
			flush()
		default:
			cur.WriteRune(c)
		}
	}
	if quote != 0 {
		return nil, false
	}
	flush()
	return segs, true
}

// cleanSegment drops grouping, negation and redirections from one simple command.
func cleanSegment(seg string) string {
	seg = strings.Trim(strings.TrimSpace(seg), "()")
	seg = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(seg), "!"))
	var keep []string
	for _, w := range strings.Fields(seg) {
		if isRedirection(w) {
			continue
		}
		keep = append(keep, w)
	}
	return strings.Join(keep, " ")
}

func isRedirection(w string) bool {
	w = strings.TrimLeft(w, "0123456789")
	return strings.HasPrefix(w, ">") || strings.HasPrefix(w, "<")
}

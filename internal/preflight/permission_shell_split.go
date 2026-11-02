package preflight

import "strings"

// segment is one simple command cut out of a compound one.
type segment struct {
	text         string // redirections, grouping and control keywords dropped
	fileRedirect bool   // writes to a file: > f, >> f, 2> f (not /dev/null, not 2>&1)
	target       string // the file a redirection writes to, when fileRedirect
	background   bool   // ended by a single &
	grouped      bool   // inside ( ), a subshell
	control      bool   // part of a for/while/if/case construct
}

// splitSegments cuts a shell command into its simple commands at && || ; | & and
// newlines outside quotes, and drops redirections and grouping. simple is false
// when the command substitutes ($( ) or backticks), ends in a dangling && or ||,
// or does not parse, since no rule can then be matched against its parts (Claude
// Code itself refuses to split a command that ends in && or ||).
func splitSegments(s string) (segs []segment, simple bool) {
	var cur strings.Builder
	var quote rune
	sawControl := false
	flush := func(background bool) {
		seg, ctrl := cleanSegment(cur.String())
		sawControl = sawControl || ctrl
		if seg.text != "" {
			seg.background = background
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
		case c == '&' && (i == 0 || rs[i-1] != '&') && (i+1 == len(rs) || rs[i+1] != '&'):
			flush(true)
		case c == ';' || c == '|' || c == '&' || c == '\n':
			flush(false)
		default:
			cur.WriteRune(c)
		}
	}
	if quote != 0 {
		return nil, false
	}
	if t := strings.TrimSpace(s); strings.HasSuffix(t, "&&") || strings.HasSuffix(t, "||") {
		return nil, false
	}
	flush(false)
	for i := range segs {
		segs[i].control = sawControl
	}
	return segs, true
}

// splitCommand is splitSegments for callers that only need the command texts.
func splitCommand(s string) (segs []string, simple bool) {
	parts, simple := splitSegments(s)
	if !simple {
		return nil, false
	}
	for _, p := range parts {
		segs = append(segs, p.text)
	}
	return segs, true
}

// controlWords start a command inside a shell construct: they are dropped and
// the command after them is kept. controlHeads only head or close a construct
// (for x in y, done, fi) and leave nothing.
var (
	controlWords = map[string]bool{"if": true, "then": true, "else": true, "elif": true, "do": true, "while": true, "until": true, "{": true}
	controlHeads = map[string]bool{"for": true, "case": true, "select": true, "done": true, "fi": true, "esac": true, "}": true}
)

// cleanSegment drops grouping, negation, control keywords and redirections from
// one simple command and notes whether a redirection writes to a file. control
// is true when the text was part of a shell construct.
func cleanSegment(raw string) (seg segment, control bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "(") || strings.HasSuffix(raw, ")") {
		seg.grouped = true
	}
	raw = strings.Trim(raw, "()")
	raw = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "!"))
	words := shellFields(raw)
	for len(words) > 0 && controlWords[words[0]] {
		control, words = true, words[1:]
	}
	if len(words) > 0 && controlHeads[words[0]] {
		return segment{}, true
	}
	var keep []string
	for i := 0; i < len(words); i++ {
		w := strings.TrimLeft(words[i], "0123456789")
		switch {
		case strings.HasPrefix(w, ">"):
			target := strings.TrimLeft(w, ">")
			if target == "" && i+1 < len(words) {
				i++
				target = words[i]
			}
			if target != "/dev/null" && !strings.HasPrefix(target, "&") {
				seg.fileRedirect, seg.target = true, target
			}
		case strings.HasPrefix(w, "<"):
			if w == "<" && i+1 < len(words) {
				i++ // the file it reads is not part of the command
			}
		default:
			keep = append(keep, words[i])
		}
	}
	seg.text = strings.Join(keep, " ")
	return seg, control
}

// shellFields splits on whitespace outside quotes, so `echo "a > b"` has two words.
func shellFields(s string) []string {
	var words []string
	var cur strings.Builder
	var quote rune
	for _, c := range s {
		switch {
		case quote != 0:
			cur.WriteRune(c)
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
			cur.WriteRune(c)
		case c == ' ' || c == '\t':
			if cur.Len() > 0 {
				words = append(words, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(c)
		}
	}
	if cur.Len() > 0 {
		words = append(words, cur.String())
	}
	return words
}

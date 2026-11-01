// Package hook decides whether an agent's shell command should run through
// heavyq and produces the Claude Code PreToolUse response that rewrites it.
//
// The parser below is deliberately conservative. It accepts only a few
// shapes it fully understands and returns an error for anything else
// (expansions, subshells, redirections other than 2>&1, ';', a lone '&',
// several pipes). An error means "leave the command unchanged".
package hook

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// token is one shell word or operator, with its byte span in the source.
type token struct {
	val        string // unquoted value (words) or operator text
	start, end int
	op         bool
	assign     bool // NAME=... with an unquoted name: an env assignment
}

// Parsed is a command in one of the accepted safe forms:
//
//	[cd DIR &&] [NAME=VAL ...] PROG ARGS... [2>&1] [| tail|head|grep ARGS...]
type Parsed struct {
	Src      string
	Env      []string // NAME=VAL assignments before the program
	Words    []string // program and arguments, unquoted
	EnvStart int      // byte offset where the assignments (or program) start
	ProgAt   int      // byte offset of the program word
}

var assignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// filters may receive the wrapped command's output through one pipe.
var filters = map[string]bool{"tail": true, "head": true, "grep": true}

// Parse returns the command's structure or why it is not a safe form.
func Parse(src string) (*Parsed, error) {
	toks, err := tokenize(src)
	if err != nil {
		return nil, err
	}
	var segs [][]token
	var ops []string
	cur := []token{}
	for _, t := range toks {
		if t.op && t.val != "2>&1" {
			segs, ops, cur = append(segs, cur), append(ops, t.val), []token{}
			continue
		}
		cur = append(cur, t)
	}
	segs = append(segs, cur)
	for _, s := range segs {
		if len(s) == 0 {
			return nil, errors.New("empty command segment")
		}
	}
	i := 0
	if len(ops) > 0 && ops[0] == "&&" {
		if len(segs[0]) != 2 || segs[0][0].val != "cd" || segs[0][1].op {
			return nil, errors.New("only `cd DIR && ...` may precede the command")
		}
		i = 1
	}
	if rest := ops[i:]; len(rest) > 1 || (len(rest) == 1 && rest[0] != "|") {
		return nil, errors.New("chained commands")
	}
	main := segs[i]
	for j, t := range main {
		if t.op && j != len(main)-1 {
			return nil, errors.New("2>&1 must come last")
		}
	}
	if i+1 < len(segs) {
		f := segs[i+1]
		if !filters[f[0].val] || f[0].assign {
			return nil, fmt.Errorf("pipe into %q (only tail, head, grep)", f[0].val)
		}
	}
	p := &Parsed{Src: src, EnvStart: main[0].start}
	for _, t := range main {
		switch {
		case t.op:
		case t.assign && len(p.Words) == 0:
			p.Env = append(p.Env, t.val)
		default:
			if len(p.Words) == 0 {
				p.ProgAt = t.start
			}
			p.Words = append(p.Words, t.val)
		}
	}
	if len(p.Words) == 0 {
		return nil, errors.New("no program")
	}
	return p, nil
}

// unsafe characters outside single quotes.
const unsafeChars = ";&|<>()$`\\{}!#\n\r"

func tokenize(s string) ([]token, error) {
	var out []token
	i := 0
	for i < len(s) {
		c := s[i]
		if c == ' ' || c == '\t' {
			i++
			continue
		}
		switch {
		case strings.HasPrefix(s[i:], "&&"):
			out = append(out, token{val: "&&", start: i, end: i + 2, op: true})
			i += 2
			continue
		case c == '|' && !strings.HasPrefix(s[i:], "||"):
			out = append(out, token{val: "|", start: i, end: i + 1, op: true})
			i++
			continue
		case strings.HasPrefix(s[i:], "2>&1") && (i+4 == len(s) || s[i+4] == ' ' || s[i+4] == '\t'):
			out = append(out, token{val: "2>&1", start: i, end: i + 4, op: true})
			i += 4
			continue
		}
		t, err := word(s, i)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
		i = t.end
	}
	if len(out) == 0 {
		return nil, errors.New("empty command")
	}
	return out, nil
}

// word reads one word starting at i, handling single and double quotes.
func word(s string, i int) (token, error) {
	start := i
	var b strings.Builder
	for i < len(s) && s[i] != ' ' && s[i] != '\t' {
		c := s[i]
		switch {
		case c == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return token{}, errors.New("unterminated single quote")
			}
			b.WriteString(s[i+1 : i+1+j])
			i += j + 2
		case c == '"':
			j := strings.IndexByte(s[i+1:], '"')
			if j < 0 {
				return token{}, errors.New("unterminated double quote")
			}
			in := s[i+1 : i+1+j]
			if strings.ContainsAny(in, "$`\\!") {
				return token{}, errors.New("expansion inside double quotes")
			}
			b.WriteString(in)
			i += j + 2
		case strings.IndexByte(unsafeChars, c) >= 0:
			return token{}, fmt.Errorf("shell metacharacter %q", c)
		default:
			b.WriteByte(c)
			i++
		}
	}
	return token{val: b.String(), start: start, end: i, assign: assignRe.MatchString(s[start:i])}, nil
}

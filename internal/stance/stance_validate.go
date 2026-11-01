package stance

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// namePattern is the shape of a stance name, and so of its file name.
var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// ValidName reports whether name can be a stance name.
func ValidName(name string) bool { return namePattern.MatchString(name) }

const (
	maxMinutes = 7 * 24 * 60
	maxCount   = 10000
)

// finish validates the decoded frontmatter, applies defaults and attaches the
// normalised body. stem is the file name without ".md".
func (s *Stance) finish(stem string, injectSet bool, body string) error {
	if s.Name == "" {
		s.Name = stem
	}
	switch {
	case !ValidName(s.Name):
		return fmt.Errorf("name %q must match %s", s.Name, namePattern)
	case s.Name != stem:
		return fmt.Errorf("name %q must equal the file name %q", s.Name, stem+".md")
	}
	if n := utf8.RuneCountInString(s.Description); n > maxDescriptionRunes || strings.ContainsAny(s.Description, "\r\n") {
		return fmt.Errorf("description must be one line of at most %d characters", maxDescriptionRunes)
	}
	if n := utf8.RuneCountInString(s.Next); n > maxNextRunes || strings.ContainsAny(s.Next, "\r\n") {
		return fmt.Errorf("next must be one line of at most %d characters", maxNextRunes)
	}
	if !injectSet {
		s.InjectTokens = DefaultInjectTokens
	}
	if s.InjectTokens < MinInjectTokens || s.InjectTokens > MaxInjectTokens {
		return fmt.Errorf("inject_tokens %d out of range %d-%d", s.InjectTokens, MinInjectTokens, MaxInjectTokens)
	}
	if err := s.validateTriggers(); err != nil {
		return err
	}
	body = strings.Trim(body, "\n \t")
	switch {
	case body == "":
		return errors.New("empty body")
	case len(body) > MaxBodyBytes:
		return fmt.Errorf("body is %d bytes, the limit is %d", len(body), MaxBodyBytes)
	case strings.ContainsRune(body, 0):
		return errors.New("body contains a NUL byte")
	}
	if _, err := coreLines(body); err != nil {
		return err
	}
	s.Body = body
	return nil
}

func (s *Stance) validateTriggers() error {
	for _, c := range []struct {
		key      string
		v, limit int
	}{
		{"cooldown_minutes", s.CooldownMinutes, maxMinutes},
		{"exit.timebox_minutes", s.Exit.TimeboxMinutes, maxMinutes},
		{"enter.same_failure_class_across_trees", s.Enter.SameFailureClassAcrossTrees, maxCount},
		{"enter.churn_reverts", s.Enter.ChurnReverts, maxCount},
	} {
		if c.v < 0 || c.v > c.limit {
			return fmt.Errorf("%s %d out of range 0-%d", c.key, c.v, c.limit)
		}
	}
	if st := s.Enter.Stall; st != nil {
		if st.Minutes < 0 || st.Minutes > maxMinutes || st.Runs < 0 || st.Runs > maxCount {
			return fmt.Errorf("enter.stall minutes must be 0-%d and runs 0-%d", maxMinutes, maxCount)
		}
		if st.Minutes == 0 && st.Runs == 0 {
			return errors.New("enter.stall needs minutes or runs above 0")
		}
	}
	kw := s.Enter.Keywords
	if len(kw) > maxKeywords {
		return fmt.Errorf("enter.keywords has %d entries, the limit is %d", len(kw), maxKeywords)
	}
	for _, k := range kw {
		if strings.TrimSpace(k) == "" || utf8.RuneCountInString(k) > maxKeywordRunes || strings.ContainsAny(k, "\r\n") {
			return fmt.Errorf("enter.keywords entry %q must be one non-empty line of at most %d characters", k, maxKeywordRunes)
		}
	}
	return nil
}

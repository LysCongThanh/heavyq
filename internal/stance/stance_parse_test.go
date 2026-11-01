package stance

import (
	"strings"
	"testing"
)

const validFile = `+++
name = "demo"
description = "A demo stance."
cooldown_minutes = 45
inject_tokens = 120

[enter]
same_failure_class_across_trees = 3
churn_reverts = 2
keywords = ["demo", "đột phá"]

[enter.stall]
minutes = 40
runs = 8
without_proof = true

[exit]
decision_record = true
timebox_minutes = 30
+++

First line.

- item one
`

func TestStanceParseValid(t *testing.T) {
	s, err := Parse("/x/demo.md", []byte(validFile))
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "demo" || s.Description != "A demo stance." || s.CooldownMinutes != 45 || s.InjectTokens != 120 {
		t.Fatalf("scalars: %+v", s)
	}
	if st := s.Enter.Stall; st == nil || st.Minutes != 40 || st.Runs != 8 || !st.WithoutProof {
		t.Fatalf("stall: %+v", st)
	}
	if s.Enter.SameFailureClassAcrossTrees != 3 || s.Enter.ChurnReverts != 2 || len(s.Enter.Keywords) != 2 || s.Enter.Keywords[1] != "đột phá" {
		t.Fatalf("enter: %+v", s.Enter)
	}
	if !s.Exit.DecisionRecord || s.Exit.TimeboxMinutes != 30 {
		t.Fatalf("exit: %+v", s.Exit)
	}
	if s.Body != "First line.\n\n- item one" {
		t.Fatalf("body = %q", s.Body)
	}
}

func TestStanceParseDefaultsNameFromFileAndInjectTokens(t *testing.T) {
	s, err := Parse("/x/mine.md", []byte("+++\n+++\nHello\n"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "mine" || s.InjectTokens != DefaultInjectTokens || s.Body != "Hello" {
		t.Fatalf("%+v", s)
	}
}

func TestStanceParseToleratesBOMAndCRLF(t *testing.T) {
	data := "\xef\xbb\xbf+++\r\nname = \"demo\"\r\n+++\r\nline one\r\nline two\r\n"
	s, err := Parse("demo.md", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if s.Body != "line one\nline two" {
		t.Fatalf("body = %q", s.Body)
	}
}

// Errors name the file and the 1-based line in the stance file itself, not
// in the frontmatter slice.
func TestStanceParseErrors(t *testing.T) {
	long := strings.Repeat("x", MaxBodyBytes+1)
	cases := []struct {
		name, data, want string
	}{
		{"no opening fence", "name = \"demo\"\nbody\n", "demo.md:1: file must start with a +++"},
		{"empty file", "", "demo.md:1: file must start with a +++"},
		{"not closed", "+++\nname = \"demo\"\nbody\n", "frontmatter is not closed"},
		{"unknown top key", "+++\nname = \"demo\"\ncooldown_minute = 5\n+++\nb\n", `demo.md:3: unknown key "cooldown_minute"`},
		{"unknown nested key", "+++\n[enter]\nkeywords = [\"a\"]\nbogus = 1\n+++\nb\n", `demo.md:4: unknown key "enter.bogus"`},
		{"unknown key in inline table", "+++\n[enter]\nstall = {minutes = 1, nope = 2}\n+++\nb\n", `demo.md:3: unknown key "enter.stall.nope"`},
		{"unknown table", "+++\n\n[extra]\nx = 1\n+++\nb\n", `demo.md:3: unknown key "extra"`},
		{"toml syntax", "+++\nname = \n+++\nb\n", "demo.md:2:"},
		{"wrong type", "+++\ncooldown_minutes = \"soon\"\n+++\nb\n", "cooldown_minutes"},
		{"name mismatch", "+++\nname = \"other\"\n+++\nb\n", `name "other" must equal the file name "demo.md"`},
		{"bad name", "+++\nname = \"Demo X\"\n+++\nb\n", "must match"},
		{"inject too big", "+++\ninject_tokens = 601\n+++\nb\n", "inject_tokens 601 out of range"},
		{"inject too small", "+++\ninject_tokens = 0\n+++\nb\n", "inject_tokens 0 out of range"},
		{"negative cooldown", "+++\ncooldown_minutes = -1\n+++\nb\n", "cooldown_minutes -1 out of range"},
		{"empty stall", "+++\n[enter.stall]\nwithout_proof = true\n+++\nb\n", "enter.stall needs minutes or runs"},
		{"blank keyword", "+++\n[enter]\nkeywords = [\" \"]\n+++\nb\n", "enter.keywords entry"},
		{"multiline description", "+++\ndescription = \"\"\"a\nb\"\"\"\n+++\nb\n", "description must be one line"},
		{"empty body", "+++\nname = \"demo\"\n+++\n \n\n", "empty body"},
		{"oversize body", "+++\n+++\n" + long, "body is 32769 bytes, the limit is 32768"},
		{"nul in body", "+++\n+++\nab\x00cd\n", "NUL byte"},
		{"invalid utf8", "+++\n+++\nab\xffcd\n", "not valid UTF-8"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse("demo.md", []byte(c.data))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to contain %q", err, c.want)
			}
		})
	}
}

func TestStanceParseBodyAtTheLimitIsAccepted(t *testing.T) {
	body := strings.Repeat("a", MaxBodyBytes)
	s, err := Parse("demo.md", []byte("+++\n+++\n"+body))
	if err != nil || len(s.Body) != MaxBodyBytes {
		t.Fatalf("err=%v len=%d", err, len(s.Body))
	}
}

// A body in any language survives parsing byte for byte (modulo trimming).
func TestStanceVietnameseBodyRoundTrip(t *testing.T) {
	body := "Bạn đang tối ưu cách làm hiện tại. Hãy lùi lại một bước.\n\n- Trả lời trước: \"nếu làm lại từ đầu thì giải pháp tốt nhất là gì?\"\n- Liệt kê ít nhất ba phương án, gồm một phương án đột phá."
	data := "+++\nname = \"dot-pha\"\ndescription = \"Tư duy đột phá\"\n+++\n" + body + "\n"
	s, err := Parse("/home/u/dot-pha.md", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if s.Body != body || s.Description != "Tư duy đột phá" {
		t.Fatalf("round trip lost text: %q / %q", s.Body, s.Description)
	}
}

package config

import (
	"strings"
	"testing"
)

func TestParseSiblingSpec(t *testing.T) {
	for _, c := range []struct{ spec, repo, ref string }{
		{"lib", "lib", "HEAD"},
		{"lib@v1.2.0", "lib", "v1.2.0"},
		{"shared-schema@feature/x", "shared-schema", "feature/x"},
		{"../core.lib@abc123", "../core.lib", "abc123"},
		{"/srv/repos/lib", "/srv/repos/lib", "HEAD"},
		{"a_b-c.d", "a_b-c.d", "HEAD"},
	} {
		repo, ref, err := ParseSiblingSpec(c.spec)
		if err != nil || repo != c.repo || ref != c.ref {
			t.Errorf("ParseSiblingSpec(%q) = %q, %q, %v; want %q, %q", c.spec, repo, ref, err, c.repo, c.ref)
		}
	}
	for _, spec := range []string{
		"", "@HEAD", "lib@", ".", "..", "...", "-lib", ".hidden", "li b", "li$b", "lib;x", "li\nb",
		"lib@-x", "lib@a b", "lib@a\nb", "lib@--upload-pack=x", strings.Repeat("a", 65),
	} {
		if repo, ref, err := ParseSiblingSpec(spec); err == nil {
			t.Errorf("ParseSiblingSpec(%q) = %q, %q; want an error", spec, repo, ref)
		}
	}
}

func TestRunConfigNeedGHRepos(t *testing.T) {
	ok := RunConfig{NeedGHRepos: []string{"app", "core-lib"}}
	if err := ok.normalize(); err != nil {
		t.Fatal(err)
	}
	c := &Config{Run: ok}
	if !c.NeedsGH("app") || c.NeedsGH("other") || c.NeedsGH("") {
		t.Fatal("NeedsGH does not follow the list")
	}
	for _, bad := range [][]string{{"../x"}, {"a/b"}, {""}, {"app", "app"}, {"-x"}} {
		if err := (&RunConfig{NeedGHRepos: bad}).normalize(); err == nil {
			t.Errorf("need_gh_repos %q accepted", bad)
		}
	}
}

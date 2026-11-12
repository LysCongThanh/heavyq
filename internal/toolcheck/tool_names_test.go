package toolcheck

import (
	"reflect"
	"strings"
	"testing"
)

func TestValidNamesArePlainCommandNames(t *testing.T) {
	for _, ok := range []string{"go", "python3", "g++", "x86_64-linux-gnu-gcc", "docker-compose", "envsubst", "_x", "pg_ctl"} {
		if !Valid(ok) {
			t.Errorf("%q must be a valid tool name", ok)
		}
	}
	for _, bad := range []string{"", "-rf", ".hidden", "../x", "a/b", "/usr/bin/go", "a b", "x;y", "$(id)", "`id`", "a'b", "tab\tx",
		"new\nline", strings.Repeat("x", 65)} {
		if Valid(bad) {
			t.Errorf("%q must not be a valid tool name", bad)
		}
	}
}

func TestParseListFailsClosed(t *testing.T) {
	got, err := ParseList("go,node,go")
	if err != nil || !reflect.DeepEqual(got, []string{"go", "node"}) {
		t.Fatalf("ParseList = %q, %v; want go, node (each once, in order)", got, err)
	}
	for _, bad := range []string{"", ",", "go,", ",go", "go,,node", "../x", "a b", "go;rm"} {
		if _, err := ParseList(bad); err == nil {
			t.Errorf("ParseList(%q) must fail", bad)
		}
	}
	many := strings.TrimSuffix(strings.Repeat("t,", MaxTools+1), ",")
	if _, err := ParseList(strings.ReplaceAll(many, "t", "x")); err != nil {
		t.Fatalf("one name %d times is one tool: %v", MaxTools+1, err)
	}
	var names []string
	for i := 0; i <= MaxTools; i++ {
		names = append(names, "t"+strings.Repeat("x", i))
	}
	if _, err := ParseList(strings.Join(names, ",")); err == nil {
		t.Fatalf("more than %d tools must fail", MaxTools)
	}
}

func TestCheckListValidatesAFileList(t *testing.T) {
	if err := CheckList([]string{"envsubst", "initdb"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]string{{""}, {"a/b"}, {"go", "-x"}, {"pg,psql"}} {
		if err := CheckList(bad); err == nil {
			t.Errorf("CheckList(%q) must fail", bad)
		}
	}
}

func TestReasonNamesTheToolsAndEnvPath(t *testing.T) {
	if got := Reason([]string{"envsubst", "initdb"}); got != "lacks envsubst, initdb (see its env_path)" {
		t.Fatalf("Reason = %q", got)
	}
}

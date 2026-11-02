package pools

import (
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestParseRange(t *testing.T) {
	got, err := ParseRange(" 4100 - 4103 ")
	if err != nil || !reflect.DeepEqual(got, []string{"4100", "4101", "4102", "4103"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	if got, err := ParseRange("7-7"); err != nil || len(got) != 1 {
		t.Fatalf("single value range: %v, %v", got, err)
	}
	for _, bad := range []string{"", "5-3", "a-b", "1-", "1-2-3", "-5", "4100"} {
		if _, err := ParseRange(bad); err == nil {
			t.Errorf("ParseRange(%q) accepted", bad)
		}
	}
	if _, err := ParseRange("1-70000"); err == nil {
		t.Error("a range over the limit was accepted")
	}
}

// decodePools parses TOML text the way config and heavyq.toml do.
func decodePools(t *testing.T, text string) ([]Pool, error) {
	t.Helper()
	var f struct {
		Pool []Spec `toml:"pool"`
	}
	if _, err := toml.Decode(text, &f); err != nil {
		t.Fatal(err)
	}
	return Build(f.Pool)
}

func TestBuildFromTOML(t *testing.T) {
	ps, err := decodePools(t, `
[[pool]]
name = "port"
values = "4100-4102"
[[pool]]
name = "pg"
values = ["app_wt1", "app_wt2"]
[[pool]]
name = "browser"
size = 2
[[pool]]
name = "web_port"
values = [8080, 8081]
[[pool]]
name = "seat"
values = "1-3"
port = false
`)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		name string
		vals []string
		port bool
	}{
		{"port", []string{"4100", "4101", "4102"}, true},
		{"pg", []string{"app_wt1", "app_wt2"}, false},
		{"browser", []string{"1", "2"}, false},
		{"web_port", []string{"8080", "8081"}, true},
		{"seat", []string{"1", "2", "3"}, false},
	}
	for i, w := range want {
		if ps[i].Name != w.name || !reflect.DeepEqual(ps[i].Values, w.vals) || ps[i].Port != w.port {
			t.Errorf("pool %d = %+v, want %+v", i, ps[i], w)
		}
	}
}

func TestBuildRefusesUnsafeOrAmbiguous(t *testing.T) {
	for name, text := range map[string]string{
		"upper-case name":  "[[pool]]\nname = \"PG\"\nsize = 1\n",
		"leading digit":    "[[pool]]\nname = \"1pg\"\nsize = 1\n",
		"too long":         "[[pool]]\nname = \"" + strings.Repeat("a", 33) + "\"\nsize = 1\n",
		"reserved":         "[[pool]]\nname = \"lane\"\nsize = 1\n",
		"no values":        "[[pool]]\nname = \"pg\"\n",
		"both":             "[[pool]]\nname = \"pg\"\nsize = 2\nvalues = [\"a\"]\n",
		"size too large":   "[[pool]]\nname = \"pg\"\nsize = 5000\n",
		"unsafe value":     "[[pool]]\nname = \"pg\"\nvalues = [\"../x\"]\n",
		"duplicate value":  "[[pool]]\nname = \"pg\"\nvalues = [\"a\", \"a\"]\n",
		"empty list":       "[[pool]]\nname = \"pg\"\nvalues = []\n",
		"name as range":    "[[pool]]\nname = \"pg\"\nvalues = \"app_wt1\"\n",
		"duplicate pool":   "[[pool]]\nname = \"pg\"\nsize = 1\n[[pool]]\nname = \"pg\"\nsize = 2\n",
		"port not numeric": "[[pool]]\nname = \"port\"\nvalues = [\"a\"]\n",
		"port too big":     "[[pool]]\nname = \"port\"\nvalues = \"65000-65600\"\n",
		"float value":      "[[pool]]\nname = \"pg\"\nvalues = [1.5]\n",
	} {
		if _, err := decodePools(t, text); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestMergeRepoWins(t *testing.T) {
	user := []Pool{{Name: "pg", Values: []string{"u1"}}, {Name: "port", Values: []string{"1"}}}
	repo := []Pool{{Name: "pg", Values: []string{"r1", "r2"}}, {Name: "seat", Values: []string{"1"}}}
	got := Merge(user, repo)
	names := []string{}
	for _, p := range got {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "port,pg,seat" || len(got[1].Values) != 2 {
		t.Fatalf("merged = %+v", got)
	}
}

func TestParseNeeds(t *testing.T) {
	ns, err := ParseNeeds("port:2, pg ,browser:1")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(FormatNeeds(ns), ","); got != "port:2,pg,browser" {
		t.Fatalf("round trip = %q", got)
	}
	if got := ns[0].EnvNames(); !reflect.DeepEqual(got, []string{"HEAVYQ_PORT_1", "HEAVYQ_PORT_2"}) {
		t.Fatalf("env names = %v", got)
	}
	if got := ns[1].EnvNames(); !reflect.DeepEqual(got, []string{"HEAVYQ_PG"}) {
		t.Fatalf("env names = %v", got)
	}
	for _, bad := range []string{"", "pg,,port", "pg:0", "pg:x", "pg:65", "PG", "lane", "pg,pg", "pg:2,pg", "port:2,port_1", "pg:"} {
		if _, err := ParseNeeds(bad); err == nil {
			t.Errorf("ParseNeeds(%q) accepted", bad)
		}
	}
	if ns, err := ParseNeedList(nil); err != nil || ns != nil {
		t.Fatalf("empty list: %v, %v", ns, err)
	}
}

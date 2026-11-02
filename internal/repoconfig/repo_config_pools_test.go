package repoconfig

import (
	"reflect"
	"strings"
	"testing"
)

func TestLoadPoolsAndGateNeeds(t *testing.T) {
	r, err := Load(newRepo(t, `
[[pool]]
name = "pg"
values = ["app_wt1", "app_wt2"]
[[pool]]
name = "port"
values = "4100-4109"

[[gate]]
name = "e2e"
run = ["npm run e2e"]
needs = ["pg", "port:2"]
remote = "auto"
`))
	if err != nil || r == nil {
		t.Fatal(r, err)
	}
	if len(r.Pools) != 2 || r.Pools[0].Name != "pg" || len(r.Pools[1].Values) != 10 {
		t.Fatalf("pools = %+v", r.Pools)
	}
	if g, _ := r.Gate("e2e"); !reflect.DeepEqual(g.Needs, []string{"pg", "port:2"}) {
		t.Fatalf("needs = %v", g.Needs)
	}
}

func TestGateNeedsAreValidated(t *testing.T) {
	for name, needs := range map[string]string{
		"bad count":     `needs = ["pg:0"]`,
		"upper case":    `needs = ["PG"]`,
		"twice":         `needs = ["pg", "pg:2"]`,
		"comma inside":  `needs = ["pg,port"]`,
		"named remote":  "needs = [\"pg\"]\nremote = \"box\"",
		"not a list":    `needs = "pg"`,
		"empty element": `needs = [""]`,
	} {
		body := "[[gate]]\nname = \"g\"\nrun = [\"true\"]\n" + needs + "\n"
		_, err := Load(newRepo(t, body))
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if !strings.Contains(err.Error(), FileName) {
			t.Errorf("%s: error does not name the file: %v", name, err)
		}
	}
}

func TestBadPoolIsAnErrorNamingTheFile(t *testing.T) {
	_, err := Load(newRepo(t, "[[pool]]\nname = \"pg\"\n"))
	if err == nil || !strings.Contains(err.Error(), "[[pool]] #1") {
		t.Fatalf("err = %v", err)
	}
}

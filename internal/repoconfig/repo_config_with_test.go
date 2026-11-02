package repoconfig

import (
	"reflect"
	"strings"
	"testing"
)

func TestLoadGateWithAndNeedGH(t *testing.T) {
	r, err := Load(newRepo(t, `
[[gate]]
name = "contract"
run = ["make contract"]
with = ["shared-schema@v2.1.0", "core-lib", "../tools/linters@main"]
need_gh = true
remote = "auto"
`))
	if err != nil || r == nil {
		t.Fatal(r, err)
	}
	g, _ := r.Gate("contract")
	if !reflect.DeepEqual(g.With, []string{"shared-schema@v2.1.0", "core-lib", "../tools/linters@main"}) || !g.NeedGH {
		t.Fatalf("gate = %+v", g)
	}
}

func TestGateWithAndNeedGHAreValidated(t *testing.T) {
	for name, body := range map[string]string{
		"unsafe name":        `with = ["bad name"]`,
		"dot name":           `with = [".."]`,
		"empty ref":          `with = ["lib@"]`,
		"option-like ref":    `with = ["lib@-x"]`,
		"twice":              `with = ["lib", "lib@v1"]`,
		"not a list":         `with = "lib"`,
		"need_gh remote":     "need_gh = true\nremote = \"box\"",
		"need_gh not a bool": `need_gh = "yes"`,
	} {
		_, err := Load(newRepo(t, "[[gate]]\nname = \"g\"\nrun = [\"true\"]\n"+body+"\n"))
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if name == "need_gh remote" && !strings.Contains(err.Error(), "need_gh") {
			t.Errorf("%s: unhelpful error %v", name, err)
		}
	}
}

package config

import (
	"strings"
	"testing"
)

func TestPoolsLoadFromUserConfig(t *testing.T) {
	c, err := Load(writeConfig(t, `
[[pool]]
name = "port"
values = "4100-4199"
[[pool]]
name = "pg"
values = ["app_wt1", "app_wt2", "app_wt3"]
[[pool]]
name = "browser"
size = 2
`), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Pools) != 3 || len(c.Pools[0].Values) != 100 || !c.Pools[0].Port || c.Pools[1].Port || len(c.Pools[2].Values) != 2 {
		t.Fatalf("pools = %+v", c.Pools)
	}
}

func TestPoolsRefuseBadDefinitions(t *testing.T) {
	for name, body := range map[string]string{
		"bad name":     "[[pool]]\nname = \"Port\"\nsize = 1\n",
		"no values":    "[[pool]]\nname = \"pg\"\n",
		"unknown key":  "[[pool]]\nname = \"pg\"\nsize = 1\ncolour = \"red\"\n",
		"upper-case":   "[[POOL]]\nname = \"pg\"\nsize = 1\n",
		"backwards":    "[[pool]]\nname = \"port\"\nvalues = \"4200-4100\"\n",
		"duplicate":    "[[pool]]\nname = \"pg\"\nsize = 1\n[[pool]]\nname = \"pg\"\nsize = 1\n",
		"not a number": "[[pool]]\nname = \"port\"\nvalues = [\"http\"]\n",
	} {
		if _, err := Load(writeConfig(t, body), true); err == nil {
			t.Errorf("%s: accepted", name)
		} else if !strings.Contains(err.Error(), "pool") && !strings.Contains(err.Error(), "unknown key") {
			t.Errorf("%s: error does not say what is wrong: %v", name, err)
		}
	}
}

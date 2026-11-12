package repoconfig

import "testing"

func TestLoadItemsTick(t *testing.T) {
	if got := DefaultItems().Tick; got != TickAuto {
		t.Fatalf("default tick = %q", got)
	}
	for _, c := range []struct{ body, want string }{
		{"[items]\niterations = 2\n", TickAuto},
		{"[items]\ntick = \"auto\"\n", TickAuto},
		{"[items]\ntick = \"always\"\n", TickAlways},
		{"[items]\ntick = \"never\"\niterations = 2\n", TickNever},
	} {
		r, err := Load(newRepo(t, c.body))
		if err != nil || r.ItemLimits().Tick != c.want {
			t.Fatalf("%q: tick = %q, %v; want %q", c.body, r.ItemLimits().Tick, err, c.want)
		}
	}
	var none *Repo
	if none.ItemLimits().Tick != TickAuto {
		t.Fatalf("no heavyq.toml: %q", none.ItemLimits().Tick)
	}
}

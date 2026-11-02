package repoconfig

import (
	"strings"
	"testing"
)

func TestLoadItemsDefaultsAndValues(t *testing.T) {
	r, err := Load(newRepo(t, "[[gate]]\nname = \"a\"\nrun = [\"x\"]\n"))
	if err != nil || r.ItemLimits() != DefaultItems() {
		t.Fatalf("no [items]: %+v, %v", r.ItemLimits(), err)
	}
	r, err = Load(newRepo(t, "[items]\niterations = 5\n"))
	if got := r.ItemLimits(); err != nil || got.Iterations != 5 || got.MaxProofShare != DefaultMaxProofShare {
		t.Fatalf("iterations only: %+v, %v", got, err)
	}
	r, err = Load(newRepo(t, "[items]\nmax_proof_share = 0.5\niterations = 2\n"))
	if got := r.ItemLimits(); err != nil || got.Iterations != 2 || got.MaxProofShare != 0.5 {
		t.Fatalf("both keys: %+v, %v", got, err)
	}
	var none *Repo // a repository with no heavyq.toml
	if none.ItemLimits() != DefaultItems() {
		t.Fatalf("nil repo: %+v", none.ItemLimits())
	}
}

func TestLoadRejectsInvalidItems(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"share zero", "[items]\nmax_proof_share = 0\n", "max_proof_share 0 must be above 0"},
		{"share over one", "[items]\nmax_proof_share = 1.5\n", "max_proof_share 1.5 must be above 0 and at most 1"},
		{"share negative", "[items]\nmax_proof_share = -1\n", "max_proof_share"},
		{"iterations zero", "[items]\niterations = 0\n", "iterations 0 must be between 1 and 20"},
		{"iterations huge", "[items]\niterations = 99\n", "iterations 99 must be between 1 and 20"},
		{"unknown key", "[items]\nmax_share = 1\n", `unknown key "items.max_share"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(newRepo(t, c.body))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v, want it to contain %q", err, c.want)
			}
		})
	}
}

package repoconfig

import (
	"strings"
	"testing"
)

func TestLoadProofRemote(t *testing.T) {
	r, err := Load(newRepo(t, `
[proof]
remote = "linuxi"

[[gate]]
name = "plain"
run = ["true"]
[[gate]]
name = "pinned"
run = ["true"]
remote = "local"
`))
	if err != nil || r == nil {
		t.Fatal(r, err)
	}
	if r.Proof.Remote != "linuxi" {
		t.Fatalf("proof remote = %q", r.Proof.Remote)
	}
	plain, _ := r.Gate("plain")
	pinned, _ := r.Gate("pinned")
	if plain.RemoteSet || plain.Remote != DefaultRemote {
		t.Fatalf("a gate without a remote key follows [proof]: %+v", plain)
	}
	if !pinned.RemoteSet || pinned.Remote != "local" {
		t.Fatalf("an explicit remote = \"local\" is the gate's own choice: %+v", pinned)
	}
}

func TestLoadProofAbsentIsEmpty(t *testing.T) {
	r, err := Load(newRepo(t, "[[gate]]\nname = \"g\"\nrun = [\"true\"]\n"))
	if err != nil || r == nil || r.Proof.Remote != "" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestLoadProofIsValidated(t *testing.T) {
	for name, body := range map[string]string{
		"bad name":    "[proof]\nremote = \"a b\"\n",
		"shell chars": "[proof]\nremote = \"x;rm\"\n",
		"unknown key": "[proof]\nwhere = \"auto\"\n",
		"not a table": "proof = \"auto\"\n",
	} {
		_, err := Load(newRepo(t, body+"[[gate]]\nname = \"g\"\nrun = [\"true\"]\n"))
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if !strings.Contains(err.Error(), FileName) {
			t.Errorf("%s: error does not name the file: %v", name, err)
		}
	}
}

// A gate's remote and [proof] remote may be an ordered list of machines; the
// remotes in it are checked against the user config when the gate runs.
func TestLoadProofRemoteList(t *testing.T) {
	r, err := Load(newRepo(t, `
[proof]
remote = "mac,local,linuxi"

[[gate]]
name = "remote-only"
run = ["true"]
remote = "mac,linuxi"
[[gate]]
name = "pooled"
run = ["true"]
remote = "mac,local"
needs = ["pg"]

[[pool]]
name = "pg"
size = 1
`))
	if err != nil || r == nil {
		t.Fatal(r, err)
	}
	if r.Proof.Remote != "mac,local,linuxi" {
		t.Fatalf("proof remote = %q", r.Proof.Remote)
	}
	for body, want := range map[string]string{
		"[proof]\nremote = \"auto,mac\"\n": "cannot be in a list",
		"[proof]\nremote = \"mac,mac\"\n":  "listed twice",
		"[[gate]]\nname = \"x\"\nrun = [\"true\"]\nremote = \"mac,linuxi\"\nneeds = [\"pg\"]\n[[pool]]\nname = \"pg\"\nsize = 1\n": "not possible",
		"[[gate]]\nname = \"x\"\nrun = [\"true\"]\nremote = \"mac,linuxi\"\nneed_gh = true\n":                                      "not possible",
	} {
		if _, err := Load(newRepo(t, body+"[[gate]]\nname = \"g\"\nrun = [\"true\"]\n")); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v must mention %q", body, err, want)
		}
	}
}

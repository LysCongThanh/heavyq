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

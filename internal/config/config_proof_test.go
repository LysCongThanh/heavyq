package config

import (
	"strings"
	"testing"
)

const proofRemotes = "[[remote]]\nname = \"box\"\nssh = \"box\"\n"

func TestProofRemoteDefaultsToUnset(t *testing.T) {
	c, err := Load("", false)
	if err != nil || c.Proof.Remote != "" {
		t.Fatalf("%+v %v", c.Proof, err)
	}
}

func TestProofRemoteAcceptsLocalAutoAndConfiguredRemote(t *testing.T) {
	for _, v := range []string{"local", "auto", "box"} {
		c, err := Load(writeConfig(t, proofRemotes+"[proof]\nremote = \""+v+"\"\n"), true)
		if err != nil || c.Proof.Remote != v {
			t.Errorf("remote %q: %+v %v", v, c.Proof, err)
		}
	}
}

func TestProofRemoteIsValidated(t *testing.T) {
	cases := map[string]string{
		proofRemotes + "[proof]\nremote = \"gone\"\n":  "gone",
		"[proof]\nremote = \"box\"\n":                  "no [[remote]] is configured",
		proofRemotes + "[proof]\nremote = \"Local\"\n": "Local",
	}
	for body, want := range cases {
		if _, err := Load(writeConfig(t, body), true); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v must mention %q", body, err, want)
		}
	}
}

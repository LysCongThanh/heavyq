package config

import (
	"strings"
	"testing"
)

const twoProofRemotes = proofRemotes + "[[remote]]\nname = \"box2\"\nssh = \"box2\"\n"

func TestMachineListShape(t *testing.T) {
	if ms, err := CheckMachineList("box,local,box2"); err != nil || strings.Join(ms, " ") != "box local box2" {
		t.Fatalf("an ordered list keeps its order: %v %v", ms, err)
	}
	for v, want := range map[string]string{
		"box":        "not a list",
		"auto,box":   "cannot be in a list",
		"box,box":    "listed twice",
		"box, local": "no spaces",
		"box,":       `"" is not`,
		",box":       `"" is not`,
		"box,x;rm":   "x;rm",
	} {
		if _, err := CheckMachineList(v); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v must mention %q", v, err, want)
		}
	}
}

func TestTargetMayRunLocally(t *testing.T) {
	for v, want := range map[string]bool{
		"local": true, "auto": true, "box": false,
		"box,local": true, "local,box": true, "box,box2": false,
	} {
		if got := TargetMayRunLocally(v); got != want {
			t.Errorf("TargetMayRunLocally(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestProofRemoteAcceptsAMachineList(t *testing.T) {
	for _, v := range []string{"box,box2", "box2,local,box", "local,box"} {
		c, err := Load(writeConfig(t, twoProofRemotes+"[proof]\nremote = \""+v+"\"\n"), true)
		if err != nil || c.Proof.Remote != v {
			t.Errorf("remote %q: %+v %v", v, c.Proof, err)
		}
	}
	for v, want := range map[string]string{
		"box,gone": "gone",
		"auto,box": "cannot be in a list",
		"box,box":  "listed twice",
	} {
		if _, err := Load(writeConfig(t, twoProofRemotes+"[proof]\nremote = \""+v+"\"\n"), true); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v must mention %q", v, err, want)
		}
	}
}

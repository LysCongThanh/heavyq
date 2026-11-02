package cli

import (
	"strings"
	"testing"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
)

func proofCfg(t *testing.T, userRemote string) *config.Config {
	t.Helper()
	cfg := &config.Config{
		Remotes: []config.MachineConfig{{Name: "box"}, {Name: "mac"}},
		Proof:   config.ProofConfig{Remote: userRemote},
	}
	return cfg
}

func TestResolveProofRemotePrecedence(t *testing.T) {
	repoAuto := &repoconfig.Repo{Proof: repoconfig.Proof{Remote: "auto"}}
	gatePinned := &repoconfig.Gate{Remote: "mac", RemoteSet: true}
	gatePlain := &repoconfig.Gate{Remote: "local"}
	cases := []struct {
		name  string
		user  string
		repo  *repoconfig.Repo
		gate  *repoconfig.Gate
		flag  string
		want  string
		wfrom string
	}{
		{"nothing set is local", "", nil, nil, "", "local", proofFromDefault},
		{"user config", "box", nil, nil, "", "box", proofFromUser},
		{"repo file beats user config", "box", repoAuto, nil, "", "auto", proofFromRepo},
		{"gate key beats repo file", "box", repoAuto, gatePinned, "", "mac", proofFromGate},
		{"a gate without a key follows [proof]", "box", repoAuto, gatePlain, "", "auto", proofFromRepo},
		{"flag beats everything", "box", repoAuto, gatePinned, "local", "local", proofFromFlag},
		{"flag names a remote", "", nil, nil, "mac", "mac", proofFromFlag},
		{"repo without [proof] falls to user", "box", &repoconfig.Repo{}, nil, "", "box", proofFromUser},
	}
	for _, tc := range cases {
		got, err := resolveProofRemote(proofCfg(t, tc.user), tc.repo, tc.gate, tc.flag)
		if err != nil || got.Remote != tc.want || got.From != tc.wfrom {
			t.Errorf("%s: got %+v, %v; want %s from %s", tc.name, got, err, tc.want, tc.wfrom)
		}
	}
}

func TestResolveProofRemoteRefusesUnknownRemoteAndSaysWhere(t *testing.T) {
	repo := &repoconfig.Repo{Proof: repoconfig.Proof{Remote: "ghost"}}
	_, err := resolveProofRemote(proofCfg(t, ""), repo, nil, "")
	if err == nil || !strings.Contains(err.Error(), "ghost") || !strings.Contains(err.Error(), repoconfig.FileName) {
		t.Fatalf("error must name the remote and heavyq.toml: %v", err)
	}
	if _, err := resolveProofRemote(proofCfg(t, ""), nil, nil, "ghost"); err == nil || !strings.Contains(err.Error(), "--remote") {
		t.Fatalf("a bad flag must say --remote: %v", err)
	}
}

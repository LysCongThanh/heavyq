package repoconfig

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/pools"
)

// gateName is the shape of a gate name: short, lower case, safe to use in
// lane names, file names and command lines.
var gateName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// remoteAuto lets heavyq place the gate on the best machine; the other
// non-named target is config.LocalName.
const remoteAuto = "auto"

// fill validates raw and stores the result in r.
func (r *Repo) fill(raw rawFile) error {
	seen := map[string]bool{}
	for i, rg := range raw.Gate {
		g, err := rg.validate()
		if err != nil {
			return fmt.Errorf("[[gate]] #%d%s: %w", i+1, nameHint(rg.Name), err)
		}
		if seen[g.Name] {
			return fmt.Errorf("[[gate]] #%d: duplicate gate name %q", i+1, g.Name)
		}
		seen[g.Name] = true
		r.Gates = append(r.Gates, g)
	}
	ps, err := pools.Build(raw.Pool)
	if err != nil {
		return err
	}
	r.Pools = ps
	if raw.Anchor != nil {
		a, err := raw.Anchor.validate()
		if err != nil {
			return fmt.Errorf("[anchor]: %w", err)
		}
		r.Anchor = a
	}
	if raw.Proof != nil && raw.Proof.Remote != "" {
		if err := validRemote(raw.Proof.Remote); err != nil {
			return fmt.Errorf("[proof]: %w", err)
		}
		r.Proof.Remote = raw.Proof.Remote
	}
	return nil
}

// nameHint formats the gate name for error prefixes ("" when unusable).
func nameHint(name string) string {
	if name == "" {
		return ""
	}
	return fmt.Sprintf(" (%q)", name)
}

func (rg rawGate) validate() (Gate, error) {
	if !gateName.MatchString(rg.Name) {
		return Gate{}, fmt.Errorf("name %q must match %s", rg.Name, gateName)
	}
	if len(rg.Run) == 0 {
		return Gate{}, errors.New("run must be a non-empty list of shell command strings")
	}
	for j, cmd := range rg.Run {
		switch {
		case strings.TrimSpace(cmd) == "":
			return Gate{}, fmt.Errorf("run[%d] is empty", j)
		case len(cmd) > MaxRunLen:
			return Gate{}, fmt.Errorf("run[%d] is %d bytes; the limit is %d", j, len(cmd), MaxRunLen)
		case strings.ContainsRune(cmd, 0):
			return Gate{}, fmt.Errorf("run[%d] contains a NUL byte", j)
		}
	}
	g := Gate{Name: rg.Name, Run: rg.Run, Prio: DefaultPrio, Light: rg.Light, Remote: DefaultRemote}
	if rg.Prio != nil {
		if *rg.Prio < MinPrio || *rg.Prio > MaxPrio {
			return Gate{}, fmt.Errorf("prio %d must be between %d and %d", *rg.Prio, MinPrio, MaxPrio)
		}
		g.Prio = *rg.Prio
	}
	if rg.Remote != "" {
		if err := validRemote(rg.Remote); err != nil {
			return Gate{}, err
		}
		g.Remote, g.RemoteSet = rg.Remote, true
	}
	if len(rg.Needs) > 0 {
		if _, err := pools.ParseNeedList(rg.Needs); err != nil {
			return Gate{}, fmt.Errorf("needs: %w", err)
		}
		if g.Remote != config.LocalName && g.Remote != remoteAuto {
			return Gate{}, fmt.Errorf("needs pool values of this machine, so remote %q is not possible (use %q)", g.Remote, config.LocalName)
		}
		g.Needs = rg.Needs
	}
	return g, nil
}

// validRemote accepts local, auto, or a name shaped like a configured remote.
// Whether that remote exists is checked when the gate runs, against the
// user's own config: this file cannot know it.
func validRemote(v string) error {
	if v == config.LocalName || v == remoteAuto || config.SafeName.MatchString(v) {
		return nil
	}
	return fmt.Errorf("remote %q must be %q, %q or a remote name (letters, digits, . _ -)", v, config.LocalName, remoteAuto)
}

func (ra rawAnchor) validate() (Anchor, error) {
	if ra.File != "" && !filepath.IsLocal(ra.File) {
		return Anchor{}, fmt.Errorf("file %q must be a relative path inside the repository", ra.File)
	}
	for j, glob := range ra.Extra {
		if !filepath.IsLocal(glob) {
			return Anchor{}, fmt.Errorf("extra[%d] %q must be a relative glob inside the repository", j, glob)
		}
		if _, err := filepath.Match(glob, ""); err != nil {
			return Anchor{}, fmt.Errorf("extra[%d] %q: %w", j, glob, err)
		}
	}
	return Anchor(ra), nil // same fields; the types differ only in TOML tags
}

package repoconfig

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gatepaths"
	"github.com/LysCongThanh/heavyq/internal/pools"
	"github.com/LysCongThanh/heavyq/internal/toolcheck"
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
		a, err := raw.Anchor.validate(r.Root)
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
	it, err := raw.Items.validate()
	if err != nil {
		return fmt.Errorf("[items]: %w", err)
	}
	r.Items = it
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
		if !config.TargetMayRunLocally(g.Remote) {
			return Gate{}, fmt.Errorf("needs pool values of this machine, so remote %q is not possible (use %q)", g.Remote, config.LocalName)
		}
		g.Needs = rg.Needs
	}
	if rg.NeedGH {
		if g.RemoteSet && !config.TargetMayRunLocally(g.Remote) {
			return Gate{}, fmt.Errorf("need_gh uses the GitHub login of this machine, so remote %q is not possible (use %q)", g.Remote, config.LocalName)
		}
		g.NeedGH = true
	}
	seenWith := map[string]bool{}
	for j, spec := range rg.With {
		repo, _, err := config.ParseSiblingSpec(spec)
		if err != nil {
			return Gate{}, fmt.Errorf("with[%d]: %w", j, err)
		}
		if seenWith[repo] {
			return Gate{}, fmt.Errorf("with[%d]: %q is listed twice", j, repo)
		}
		seenWith[repo] = true
	}
	g.With = rg.With
	if err := toolcheck.CheckList(rg.Tools); err != nil {
		return Gate{}, fmt.Errorf("tools: %w", err)
	}
	g.Tools = rg.Tools
	if err := gatepaths.ValidList(rg.Paths); err != nil {
		return Gate{}, fmt.Errorf("paths: %w", err)
	}
	g.Paths = rg.Paths
	return g, nil
}

// validRemote accepts local, auto, a name shaped like a configured remote, or
// an ordered list of local and such names ("box1,local,box2"). Whether a
// remote exists is checked when the gate runs, against the user's own config:
// this file cannot know it.
func validRemote(v string) error {
	if config.IsMachineList(v) {
		_, err := config.CheckMachineList(v)
		return err
	}
	if v == config.LocalName || v == remoteAuto || config.SafeName.MatchString(v) {
		return nil
	}
	return fmt.Errorf("remote %q must be %q, %q, a remote name (letters, digits, . _ -) or a list of names (a,b)", v, config.LocalName, remoteAuto)
}

// validate checks the [anchor] keys; root is the work tree the file sits in.
func (ra rawAnchor) validate(root string) (Anchor, error) {
	if ra.File != "" {
		if err := CheckAnchorFile(ra.File); err != nil {
			return Anchor{}, err
		}
		if root != "" && AnchorFileIsDirectory(root, ra.File) {
			return Anchor{}, fmt.Errorf("file %q is a directory; it must be a .md file", ra.File)
		}
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

// validate fills the defaults and range-checks the keys; a nil [items] is all defaults.
func (ri *rawItems) validate() (Items, error) {
	out := DefaultItems()
	if ri == nil {
		return out, nil
	}
	if v := ri.MaxProofShare; v != nil {
		if math.IsNaN(*v) || *v <= 0 || *v > 1 {
			return Items{}, fmt.Errorf("max_proof_share %v must be above 0 and at most 1", *v)
		}
		out.MaxProofShare = *v
	}
	if v := ri.Iterations; v != nil {
		if *v < 1 || *v > MaxIterations {
			return Items{}, fmt.Errorf("iterations %d must be between 1 and %d", *v, MaxIterations)
		}
		out.Iterations = *v
	}
	if v := ri.Tick; v != nil {
		switch *v {
		case TickAuto, TickAlways, TickNever:
			out.Tick = *v
		default:
			return Items{}, fmt.Errorf("tick %q must be %q, %q or %q", *v, TickAuto, TickAlways, TickNever)
		}
	}
	return out, nil
}

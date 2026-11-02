package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/history"
	"github.com/LysCongThanh/heavyq/internal/pools"
	"github.com/LysCongThanh/heavyq/internal/queue"
)

// runOptions are the parsed flags of `heavyq <prio> ... -- cmd`.
type runOptions struct {
	Prio     int
	Lane     string
	Light    bool
	MaxWait  time.Duration // 0 = forever
	Remote   string        // "" = default placement
	NeedsRAM bool
	Repo     string
	Ref      string
	BaseRef  string
	With     []string
	// Needs are the pool values the run holds while it runs (--needs).
	Needs []pools.Need
	Cache bool
	// Session, Item and Gate tag the run in the ledger: who ran it and why.
	// Session and Item default to $HEAVYQ_SESSION and $HEAVYQ_ITEM.
	Session string
	Item    string
	Gate    string
	// Digest prints the result digest for a person too (agent mode always does).
	Digest  bool
	Verbose bool
	Argv    []string
}

// parseRunArgs parses args starting with the priority.
func parseRunArgs(args []string, cfg *config.Config) (runOptions, error) {
	o := runOptions{Ref: "HEAD", BaseRef: cfg.Queue.BaseRef, Remote: os.Getenv("HEAVYQ_REMOTE")}
	p, err := strconv.Atoi(args[0])
	if err != nil || p < 1 || p > 9 {
		return o, fmt.Errorf("priority must be 1-9, got %q", args[0])
	}
	o.Prio = p
	o.Lane = os.Getenv("HEAVYQ_LANE")
	if o.Lane == "" {
		if wd, err := os.Getwd(); err == nil {
			o.Lane = filepath.Base(wd)
		}
	}
	rest := args[1:]
	for len(rest) > 0 && rest[0] != "--" {
		name, val, hasVal := strings.Cut(rest[0], "=")
		takeVal := func() (string, error) {
			if hasVal {
				rest = rest[1:]
				return val, nil
			}
			if len(rest) < 2 {
				return "", fmt.Errorf("%s needs a value", name)
			}
			v := rest[1]
			rest = rest[2:]
			return v, nil
		}
		var v string
		switch name {
		case "--light", "--needs-ram", "--cache", "--digest", "-v", "--verbose":
			if hasVal {
				return o, fmt.Errorf("%s takes no value", name)
			}
			rest = rest[1:]
			switch name {
			case "--light":
				o.Light = true
			case "--needs-ram":
				o.NeedsRAM = true
			case "--cache":
				o.Cache = true
			case "--digest":
				o.Digest = true
			default:
				o.Verbose = true
			}
			continue
		case "--lane", "--max-wait", "--remote", "--repo", "--ref", "--base-ref", "--with",
			"--session", "--item", "--gate", "--needs":
			if v, err = takeVal(); err != nil {
				return o, err
			}
		default:
			return o, fmt.Errorf("unknown flag %q (did you forget '--' before the command?)", rest[0])
		}
		switch name {
		case "--lane":
			o.Lane = v
		case "--max-wait":
			if o.MaxWait, err = parseWait(v); err != nil {
				return o, err
			}
		case "--remote":
			o.Remote = v
		case "--repo":
			o.Repo = v
		case "--ref":
			o.Ref = v
		case "--base-ref":
			o.BaseRef = v
		case "--with":
			o.With = append(o.With, v)
		case "--needs":
			if err := o.addNeeds(v); err != nil {
				return o, err
			}
		case "--session", "--item", "--gate":
			if !history.ValidTag(v) {
				return o, fmt.Errorf("%s %q: want 1-80 characters from A-Za-z0-9._:-", name, v)
			}
			switch name {
			case "--session":
				o.Session = v
			case "--item":
				o.Item = v
			default:
				o.Gate = v
			}
		}
	}
	if err := o.tagsFromEnv(); err != nil {
		return o, err
	}
	if len(rest) == 0 {
		return o, fmt.Errorf("missing '--' before the command")
	}
	o.Argv = rest[1:]
	if len(o.Argv) == 0 {
		return o, fmt.Errorf("missing command after '--'")
	}
	if o.BaseRef == "none" {
		o.BaseRef = ""
	}
	if o.BaseRef != "" && !config.ValidBaseRef(o.BaseRef) {
		return o, fmt.Errorf("--base-ref %q must look like remote/branch (e.g. origin/main) or none", o.BaseRef)
	}
	if o.Remote != "" && o.Remote != "local" && o.Remote != "auto" {
		if _, ok := cfg.Machine(o.Remote); !ok {
			return o, fmt.Errorf("--remote %q: not local, auto, or a [[remote]] in %s", o.Remote, cfgName(cfg))
		}
	}
	o.Lane = queue.SanitizeLane(o.Lane)
	return o, nil
}

// tagsFromEnv fills Session and Item from the environment when no flag set
// them. A malformed value is refused rather than recorded or dropped.
func (o *runOptions) tagsFromEnv() error {
	for _, t := range []struct {
		env string
		dst *string
	}{{"HEAVYQ_SESSION", &o.Session}, {"HEAVYQ_ITEM", &o.Item}} {
		v := os.Getenv(t.env)
		if *t.dst != "" || v == "" {
			continue
		}
		if !history.ValidTag(v) {
			return fmt.Errorf("$%s %q: want 1-80 characters from A-Za-z0-9._:-", t.env, v)
		}
		*t.dst = v
	}
	return nil
}

// parseWait accepts plain seconds ("90") or a Go duration ("5m").
func parseWait(v string) (time.Duration, error) {
	if n, err := strconv.Atoi(v); err == nil && n >= 0 {
		return time.Duration(n) * time.Second, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("--max-wait %q: want seconds or a duration like 5m", v)
	}
	return d, nil
}

func cfgName(cfg *config.Config) string {
	if cfg.Path == "" {
		return "the config (none loaded)"
	}
	return cfg.Path
}

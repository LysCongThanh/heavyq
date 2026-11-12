package main

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// The validators of the user config live in internal/config. The rules below repeat them from its exported
// patterns and limits, so an editor shows what heavyq would refuse at load; schema_validator_parity_test.go
// checks every rule against config.Load. A rule that spans keys (unique remote names, a [proof] remote that
// must be configured, the slot total) stays with heavyq doctor.

func patchUserConfig(root *jsonschema.Schema) {
	queue := def(root, "QueueConfig")
	baseRef := prop(queue, "base_ref")
	baseRef.Pattern = `^$|^` + regexp.QuoteMeta(config.BaseRefNone) + `$|` + config.BaseRefPattern.String()
	baseRef.Not = containing(`\.\.`)

	patchHookConfig(root)

	cache := def(root, "CacheConfig")
	atLeast(prop(cache, "max_size_mb"), 0)
	atLeast(prop(cache, "max_entry_mb"), 0)
	prop(cache, "env").Items.Pattern = config.EnvNamePattern.String()

	mentor := def(root, "MentorConfig")
	within(prop(mentor, "holdout_pct"), 0, 100)
	atLeast(prop(mentor, "min_events"), 0)
	within(prop(mentor, "min_lift_pp"), 0, 100)

	proof := def(root, "ProofConfig")
	proof.Description = "Where `heavyq item done --run` and `heavyq gate run` run proofs. The same table in a repository's heavyq.toml overrides it there."
	prop(proof, "remote").Description = proofRemoteDescription
	prop(proof, "remote").Default = config.LocalName

	runCfg := def(root, "RunConfig")
	runCfg.Description = "Defaults for `heavyq <prio> -- cmd`."
	needGH := prop(runCfg, "need_gh_repos")
	needGH.Description = "Repository directory names whose commands call the GitHub CLI with this machine's login: runs there behave as if --need-gh were given (this machine only)."
	needGH.Items.Pattern = config.SafeName.String()
	needGH.UniqueItems = true

	patchMachines(root)
	patchForeign(root)
	patchPoolSpec(root)
}

func patchHookConfig(root *jsonschema.Schema) {
	hook := def(root, "HookConfig")
	prop(hook, "permission").Enum = []any{"", "allow"}
	// One word: "" (the default), an absolute or "~/" path, or a command name on PATH (no "/", no space).
	safe := `[^` + regexp.QuoteMeta(config.HookBinaryUnsafe) + `\x00-\x1f\x7f`
	prop(hook, "binary").Pattern = `^$|^(/|~/)` + safe + `]*$|^` + safe + `\s/~]` + safe + `\s/]*$`
	rule := def(root, "HookRule")
	rule.Required = []string{"match"}
	match := prop(rule, "match")
	match.MinItems = u64(1)
	// Each prefix is one or more words, each word made of the characters MatchWordPattern allows.
	word := strings.TrimSuffix(strings.TrimPrefix(config.MatchWordPattern.String(), "^"), "$")
	match.Items.Pattern = `^\s*` + word + `(\s+` + word + `)*\s*$`
	prio := prop(rule, "priority")
	within(prio, 1, 9)
	prop(rule, "lane").Pattern = orEmpty(config.SafeName)
}

// orEmpty accepts "" (the key's "unset" value) or what the anchored re matches.
func orEmpty(re *regexp.Regexp) string { return `^$|` + re.String() }

// noneOf accepts strings without any of chars (and without the characters of the class fragment extra).
func noneOf(chars, extra string) string { return `^[^` + regexp.QuoteMeta(chars) + extra + `]*$` }

// containing is a schema that matches strings containing pattern; used under "not".
func containing(pattern string) *jsonschema.Schema { return &jsonschema.Schema{Pattern: pattern} }

func atLeast(s *jsonschema.Schema, lo int) { s.Minimum = json.Number(strconv.Itoa(lo)) }

func within(s *jsonschema.Schema, lo, hi int) {
	s.Minimum = json.Number(strconv.Itoa(lo))
	s.Maximum = json.Number(strconv.Itoa(hi))
}

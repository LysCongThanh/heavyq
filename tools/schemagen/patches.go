package main

import (
	"encoding/json"
	"strconv"

	"github.com/invopop/jsonschema"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/gatepaths"
	"github.com/LysCongThanh/heavyq/internal/pools"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
	"github.com/LysCongThanh/heavyq/internal/toolcheck"
)

// The Go types say which keys exist and their types. What only the validators know (required keys, ranges,
// shapes of names) is added here, from the exported constants of the packages where there are some.

func prop(s *jsonschema.Schema, name string) *jsonschema.Schema {
	p, ok := s.Properties.Get(name)
	if !ok {
		panic("schemagen: no property " + name + " (a key was renamed: update tools/schemagen/patches.go)")
	}
	return p
}

func def(root *jsonschema.Schema, name string) *jsonschema.Schema {
	d, ok := root.Definitions[name]
	if !ok {
		panic("schemagen: no definition " + name + " (a type was renamed: update tools/schemagen/patches.go)")
	}
	return d
}

func u64(n int) *uint64 { v := uint64(n); return &v }

func patchRepoFile(root *jsonschema.Schema) {
	gate := def(root, "rawGate")
	gate.Description = "A named list of shell commands that must all pass on the current code (heavyq gate status, heavyq gate run)."
	gate.Required = []string{"name", "run"}
	name := prop(gate, "name")
	name.Description = "Gate name: lower case, used in lane names, file names and `heavyq gate run <name>`."
	name.Pattern = `^[a-z0-9][a-z0-9_-]{0,39}$`
	run := prop(gate, "run")
	run.Description = "Shell commands, run in order with `sh -c`. Each must pass on this exact tree for the gate to be proven."
	run.MinItems = u64(1)
	run.Items.MinLength = u64(1)
	run.Items.MaxLength = u64(repoconfig.MaxRunLen)
	run.Items.Pattern = `\S`
	prio := prop(gate, "prio")
	prio.Description = "Queue priority of the gate's commands; 1 runs first."
	prio.Minimum = json.Number(strconv.Itoa(repoconfig.MinPrio))
	prio.Maximum = json.Number(strconv.Itoa(repoconfig.MaxPrio))
	prio.Default = repoconfig.DefaultPrio
	prop(gate, "light").Description = "Light job: may use the light slots and jump ahead into them."
	remote := prop(gate, "remote")
	remote.Description = `Where to run: "local", "auto" (best machine) or the name of a [[remote]] of the user config.`
	remote.Default = repoconfig.DefaultRemote
	needs := prop(gate, "needs")
	needs.Description = `Pool values every command holds while it runs, for example ["pg", "port:2"]. A gate with needs runs on this machine.`
	needs.Items.Pattern = `^[a-z][a-z0-9_]{0,31}(:[0-9]+)?$`
	with := prop(gate, "with")
	with.Description = `Sibling repositories every command needs next to this one, "name@ref" (a directory next to the repository, or a path containing "/"; ref defaults to HEAD). Passed to the run as --with: pinned to a commit, shipped with it on remote runs, and part of the proof identity.`
	with.Items.MinLength = u64(1)
	prop(gate, "need_gh").Description = "The commands use the GitHub CLI with this machine's login, so the gate runs here only (--need-gh). Not combinable with a named remote."
	tools := prop(gate, "tools")
	tools.Description = `Commands every command of the gate needs on the machine it runs on, besides the one it starts, for example ["envsubst", "initdb"]. Passed to the run as --tools: a remote without one on the PATH its jobs get is skipped.`
	tools.Items.Pattern = toolcheck.NamePattern
	paths := prop(gate, "paths")
	paths.Description = `The files the gate's proof is made of, as globs relative to the repository root: "**/*.go", "go.mod", "docs/**". A commit or an edit that changes none of the files they name keeps the gate proven. Without paths the whole tree counts. * and ? stay inside one path component, ** is a whole component, a path without a wildcard names a file or a directory with everything below it. A list that matches no committed file proves nothing.`
	paths.MaxItems = u64(gatepaths.MaxEntries)
	paths.UniqueItems = true
	paths.Items.MinLength = u64(1)
	paths.Items.MaxLength = u64(gatepaths.MaxLen)
	paths.Items.Pattern = `^[^/:!\\\[\]]([^\\\[\]]*[^/\\\[\]])?$`

	proof := def(root, "rawProof")
	proof.Description = "Where `heavyq item done --run` and `heavyq gate run` run proofs for this repository; the user config has the same table."
	prop(proof, "remote").Description = `"local" (the default), "auto" (the scheduler picks among local and the configured remotes, free slots first) or the name of a [[remote]]. A gate with its own remote key keeps it; --remote wins over both.`
	prop(proof, "remote").Default = repoconfig.DefaultRemote

	anchor := def(root, "rawAnchor")
	anchor.Description = "The anchor checklist: the work items of `heavyq item`."
	prop(anchor, "file").Description = "Checklist file, relative to the repository root (default .heavyq/anchor.md)."
	prop(anchor, "extra").Description = "Extra read-only sources, as globs relative to the repository root."

	items := def(root, "rawItems")
	items.Description = "Limits of the claim size gate (`heavyq item claim` refuses an item whose proof is expected to outlast the lease) and whether `heavyq item done` ticks the box."
	tick := prop(items, "tick")
	tick.Description = `Whether "item done" ticks the box in the anchor file: "auto" (the default) ticks unless the repository has more than one work tree, "always" ticks, "never" only records the done in the events log. Without a tick, "heavyq item sync" ticks the boxes later, once, where the anchor is committed.`
	tick.Enum = []any{repoconfig.TickAuto, repoconfig.TickAlways, repoconfig.TickNever}
	tick.Default = repoconfig.TickAuto
	share := prop(items, "max_proof_share")
	share.Description = "Share of the 30 minute lease budget the expected proof time (p50 x iterations) may use."
	share.ExclusiveMinimum = json.Number("0")
	share.Maximum = json.Number("1")
	share.Default = repoconfig.DefaultMaxProofShare
	iter := prop(items, "iterations")
	iter.Description = "Proof runs an item is expected to need before it passes; the expected proof time is the proof's p50 duration times this."
	iter.Minimum = json.Number("1")
	iter.Maximum = json.Number(strconv.Itoa(repoconfig.MaxIterations))
	iter.Default = repoconfig.DefaultIterations

	patchPoolSpec(root)
}

func patchUserConfig(root *jsonschema.Schema) {
	hook := def(root, "HookConfig")
	prop(hook, "permission").Enum = []any{"", "allow"}
	rule := def(root, "HookRule")
	rule.Required = []string{"match"}
	prop(rule, "match").MinItems = u64(1)
	prio := prop(rule, "priority")
	prio.Minimum = json.Number("1")
	prio.Maximum = json.Number("9")
	proof := def(root, "ProofConfig")
	proof.Description = "Where `heavyq item done --run` and `heavyq gate run` run proofs. The same table in a repository's heavyq.toml overrides it there."
	prop(proof, "remote").Description = `"local" (the default), "auto" (the scheduler picks among local and the configured remotes, free slots first) or the name of a [[remote]]. A gate with its own remote key keeps it; --remote wins over both.`
	prop(proof, "remote").Default = config.LocalName
	runCfg := def(root, "RunConfig")
	runCfg.Description = "Defaults for `heavyq <prio> -- cmd`."
	needGH := prop(runCfg, "need_gh_repos")
	needGH.Description = "Repository directory names whose commands call the GitHub CLI with this machine's login: runs there behave as if --need-gh were given (this machine only)."
	needGH.Items.Pattern = config.SafeName.String()
	machine := def(root, "MachineConfig")
	prop(machine, "name").Pattern = config.SafeName.String()
	prop(machine, "name").Not = &jsonschema.Schema{Enum: []any{config.RemoteAuto}}
	legacy := prop(machine, "legacy_lock_files")
	legacy.UniqueItems = true
	legacy.Items.Pattern = "^/"
	patchForeign(root)
	patchPoolSpec(root)
}

// patchForeign refines [[foreign]]: what config.normalizeForeign accepts.
func patchForeign(root *jsonschema.Schema) {
	f := def(root, "ForeignConfig")
	f.Required = []string{"name"}
	f.AnyOf = []*jsonschema.Schema{{Required: []string{"history"}}, {Required: []string{"tickets"}}}
	name := prop(f, "name")
	name.Pattern = config.SafeName.String()
	name.Not = &jsonschema.Schema{Const: "heavyq"}
	prop(f, "format").Enum = []any{config.ForeignFormatTSV}
	cols := prop(f, "columns")
	enum := make([]any, len(config.ForeignColumns))
	for i, c := range config.ForeignColumns {
		enum[i] = c
	}
	cols.Items.Enum = enum
	cols.UniqueItems = true
	exits := prop(f, "infra_exits")
	exits.Items.Minimum = json.Number("1")
	exits.Items.Maximum = json.Number("255")
}

// patchPoolSpec refines [[pool]], shared by heavyq.toml and the user config.
func patchPoolSpec(root *jsonschema.Schema) {
	spec := def(root, "Spec")
	spec.Description = "A named pool of values handed out with --needs: ports, database names, browser seats."
	spec.Required = []string{"name"}
	// A pool has a list/range of values, or a capacity.
	spec.OneOf = []*jsonschema.Schema{{Required: []string{"values"}}, {Required: []string{"size"}}}
	name := prop(spec, "name")
	name.Description = "Pool name; becomes part of the environment variable HEAVYQ_<NAME>."
	name.Pattern = `^[a-z][a-z0-9_]{0,31}$`
	prop(spec, "values").Description = `A range "4100-4199", or a list of names or integers.`
	prop(spec, "values").AnyOf = []*jsonschema.Schema{
		{Type: "string", Pattern: `^\s*\d+\s*-\s*\d+\s*$`},
		{Type: "array", MinItems: u64(1), MaxItems: u64(pools.MaxValues), Items: &jsonschema.Schema{
			AnyOf: []*jsonschema.Schema{{Type: "string", Pattern: `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`}, {Type: "integer"}},
		}},
	}
	size := prop(spec, "size")
	size.Description = "A capacity-only pool: the values are 1..size."
	size.Minimum = json.Number("1")
	size.Maximum = json.Number(strconv.Itoa(pools.MaxSize))
	prop(spec, "port").Description = `true: the values are TCP ports, probed before they are handed out. Absent: a pool called "port" or ending in "_port" is a port pool.`
}

package main

import (
	"encoding/json"
	"strconv"

	"github.com/invopop/jsonschema"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/pools"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
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

	proof := def(root, "rawProof")
	proof.Description = "Where `heavyq item done --run` and `heavyq gate run` run proofs for this repository; the user config has the same table."
	prop(proof, "remote").Description = `"local" (the default), "auto" (the scheduler picks among local and the configured remotes, free slots first) or the name of a [[remote]]. A gate with its own remote key keeps it; --remote wins over both.`
	prop(proof, "remote").Default = repoconfig.DefaultRemote

	anchor := def(root, "rawAnchor")
	anchor.Description = "The anchor checklist: the work items of `heavyq item`."
	prop(anchor, "file").Description = "Checklist file, relative to the repository root (default .heavyq/anchor.md)."
	prop(anchor, "extra").Description = "Extra read-only sources, as globs relative to the repository root."

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
	machine := def(root, "MachineConfig")
	prop(machine, "name").Pattern = config.SafeName.String()
	patchPoolSpec(root)
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

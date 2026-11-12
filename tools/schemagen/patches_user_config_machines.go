package main

import (
	"github.com/invopop/jsonschema"

	"github.com/LysCongThanh/heavyq/internal/config"
)

// remoteOnlyKeys are the MachineConfig keys that mean something for a [[remote]] only; [local] refuses them.
var remoteOnlyKeys = []string{"name", "ssh", "transport", "work_dir", "git_url", "cache_hook", "env_path", "observe_only",
	"allow_untrusted_exit"}

// runOnlyKeys are the [[remote]] keys an observe_only remote refuses: heavyq never runs anything there.
var runOnlyKeys = []string{"heavy_slots", "light_slots", "max_load_ratio", "job_mb", "light_mb", "reserve_mb",
	"git_url", "cache_hook", "env_path", "work_dir", "allow_untrusted_exit"}

// patchMachines splits config.MachineConfig, one Go type for [local] and [[remote]], into two definitions,
// so each table offers only its own keys, and adds the rules of the validators.
func patchMachines(root *jsonschema.Schema) {
	machine := def(root, "MachineConfig")
	within(prop(machine, "heavy_slots"), 0, config.MaxSlots)
	within(prop(machine, "light_slots"), 0, config.MaxSlots)
	within(prop(machine, "max_load_ratio"), 0, config.MaxLoadRatio)
	for _, k := range []string{"job_mb", "light_mb", "reserve_mb"} {
		atLeast(prop(machine, k), 0)
	}

	legacy := prop(machine, "legacy_lock_files")
	legacy.UniqueItems = true
	legacy.Items.Pattern = "^/"

	local := withoutKeys(machine, remoteOnlyKeys...)
	local.Description = "This machine: slots and RAM budgets. An absent heavy_slots is 2 and an absent light_slots 1 (0 with legacy_lock_files); an explicit 0 is kept."

	remote := withoutKeys(machine, "legacy_lock_files")
	remote.Description = "A spare machine heavyq ships runs to over SSH, by exact commit (docs/remote.md). With no slot key set it has 2 heavy slots and 0 light slots."
	remote.Required = []string{"name"}
	name := prop(remote, "name")
	name.Description = `Machine name, used in --remote and in status; unique, and not "local" or "auto" (--remote keywords).`
	name.Pattern = config.SafeName.String()
	name.Not = &jsonschema.Schema{Enum: []any{config.LocalName, config.RemoteAuto}}
	prop(remote, "ssh").Pattern = orEmpty(config.SSHAliasPattern)
	// git push reads a leading '-' as an option.
	prop(remote, "git_url").Pattern = `^$|^[^-\x00-\x1f\x7f-\x9f][^\x00-\x1f\x7f-\x9f]*$`
	workDir := prop(remote, "work_dir")
	workDir.Pattern = noneOf(config.WorkDirUnsafe, "")
	// Only a leading "~/" naming a directory under the remote home is understood.
	workDir.Not = containing(`\.\.|^~([^/]|$)|^~/+$`)
	envPath := prop(remote, "env_path")
	envPath.Pattern = noneOf(config.EnvPathUnsafe, `\x00-\x1f\x7f`)
	envPath.Not = containing(`^:+$`)
	// heavyq reaches a remote through its ssh alias, or through a custom transport and the git_url to push to.
	remote.AnyOf = []*jsonschema.Schema{
		withProperties(&jsonschema.Schema{Required: []string{"ssh"}}, "ssh", &jsonschema.Schema{MinLength: u64(1)}),
		withProperties(&jsonschema.Schema{Required: []string{"transport", "git_url"}},
			"transport", &jsonschema.Schema{MinItems: u64(1)},
			"git_url", &jsonschema.Schema{MinLength: u64(1)}),
		// An observe_only remote is only probed: a transport alone reaches it.
		withProperties(&jsonschema.Schema{Required: []string{"transport", "observe_only"}},
			"transport", &jsonschema.Schema{MinItems: u64(1)},
			"observe_only", &jsonschema.Schema{Const: true}),
	}
	refused := make([]*jsonschema.Schema, len(runOnlyKeys))
	for i, k := range runOnlyKeys {
		refused[i] = &jsonschema.Schema{Required: []string{k}}
	}
	remote.If = withProperties(&jsonschema.Schema{Required: []string{"observe_only"}}, "observe_only", &jsonschema.Schema{Const: true})
	remote.Then = &jsonschema.Schema{Not: &jsonschema.Schema{AnyOf: refused}}

	delete(root.Definitions, "MachineConfig")
	root.Definitions["LocalMachine"], root.Definitions["RemoteMachine"] = local, remote
	prop(root, "local").Ref = "#/$defs/LocalMachine"
	prop(root, "remote").Items.Ref = "#/$defs/RemoteMachine"
}

// withoutKeys returns a copy of the object schema s without the properties named in drop. The remaining
// property schemas are shared with s.
func withoutKeys(s *jsonschema.Schema, drop ...string) *jsonschema.Schema {
	out := *s
	out.Properties = jsonschema.NewProperties()
	for p := s.Properties.Oldest(); p != nil; p = p.Next() {
		if !contains(drop, p.Key) {
			out.Properties.Set(p.Key, p.Value)
		}
	}
	return &out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// withProperties sets the properties of s from name, schema pairs, in order, and returns s.
func withProperties(s *jsonschema.Schema, pairs ...any) *jsonschema.Schema {
	s.Properties = jsonschema.NewProperties()
	for i := 0; i+1 < len(pairs); i += 2 {
		s.Properties.Set(pairs[i].(string), pairs[i+1].(*jsonschema.Schema))
	}
	return s
}

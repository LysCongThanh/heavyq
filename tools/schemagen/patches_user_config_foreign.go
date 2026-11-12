package main

import (
	"encoding/json"

	"github.com/invopop/jsonschema"

	"github.com/LysCongThanh/heavyq/internal/config"
)

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
	// The shape of config.CheckSlotGlob: absolute or ~/, wildcards ("*", "[0-9]") in the base name only, which
	// does not start with "."; the loader also refuses "..", a second "*" and a base name without a wildcard.
	prop(f, "slot_globs").AdditionalProperties.Pattern = `^(/|~/)([A-Za-z0-9._-]+/)*([A-Za-z0-9_*-]|\[0-9\])([A-Za-z0-9._*-]|\[0-9\])*$`
	exits := prop(f, "infra_exits")
	exits.Items.Minimum = json.Number("1")
	exits.Items.Maximum = json.Number("255")
}

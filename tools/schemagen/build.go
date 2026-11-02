package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/LysCongThanh/heavyq/internal/config"
	"github.com/LysCongThanh/heavyq/internal/repoconfig"
	"github.com/LysCongThanh/heavyq/internal/snapshot"
)

const (
	rawBase = "https://raw.githubusercontent.com/LysCongThanh/heavyq/main/schema/"
	// Draft 7 is what editors support most widely (Taplo, VS Code, SchemaStore's lowest common draft).
	draft07 = "http://json-schema.org/draft-07/schema#"
)

// schemaFile describes one output file.
type schemaFile struct {
	File        string
	Title       string
	Description string
	Type        any
	TOML        bool                          // true: keys come from `toml` tags and unknown keys are refused
	Patch       func(root *jsonschema.Schema) // refinements the Go types cannot carry
}

func schemaFiles() []schemaFile {
	return []schemaFile{
		{
			File: "heavyq.schema.json", Title: "heavyq.toml", TOML: true, Type: repoconfig.FileLayout{},
			Description: "heavyq.toml at the repository root: the gates that say what \"proven\" means, named pools and the anchor checklist.",
			Patch:       patchRepoFile,
		},
		{
			File: "config.schema.json", Title: "heavyq user config", TOML: true, Type: config.Config{},
			Description: "heavyq's user configuration, ~/.config/heavyq/config.toml: slots, RAM budgets, remotes, hook rules, cache and pools. Every key is optional.",
			Patch:       patchUserConfig,
		},
		{
			File: "status.schema.json", Title: "heavyq status --json", Type: snapshot.Snapshot{},
			Description: "The output of `heavyq status --json` (schema 1). Fields may be added within schema 1; readers should ignore unknown ones.",
		},
	}
}

// build reflects one schema and returns it as indented JSON with a trailing newline.
func build(f schemaFile) ([]byte, error) {
	r := &jsonschema.Reflector{ExpandedStruct: true, DoNotReference: false}
	if f.TOML {
		r.FieldNameTag = "toml"
		r.RequiredFromJSONSchemaTags = true // every key is optional unless a patch says otherwise
		r.Mapper = func(t reflect.Type) *jsonschema.Schema {
			if t == reflect.TypeOf(config.Duration{}) {
				return durationSchema()
			}
			return nil
		}
	} else {
		r.AllowAdditionalProperties = true // `status --json` grows within schema 1
	}
	if err := r.AddGoComments("github.com/LysCongThanh/heavyq", "internal"); err != nil {
		return nil, fmt.Errorf("reading Go comments: %w", err)
	}
	root := r.Reflect(f.Type)
	root.Version = draft07
	root.ID = jsonschema.ID(rawBase + f.File)
	root.Title = f.Title
	root.Description = f.Description
	if f.Patch != nil {
		f.Patch(root)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // keep <name> readable
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	raw := buf.Bytes()
	names := definitionNames
	if !f.TOML {
		names = nil
	}
	return toDraft07(raw, names)
}

// definitionNames are friendlier names for the types of the TOML schemas (the Go ones are unexported or
// too generic): Go type name -> definition name.
var definitionNames = map[string]string{"rawGate": "Gate", "rawAnchor": "Anchor", "Spec": "Pool"}

// toDraft07 renames $defs to definitions (the draft 7 spelling), renames definitions and indents the result.
// Renaming applies to quoted definition keys and to the last segment of references only; the TOML schemas
// are the only callers with names, and they have no other string equal to a Go type name.
func toDraft07(raw []byte, names map[string]string) ([]byte, error) {
	s := strings.ReplaceAll(string(raw), `"$defs"`, `"definitions"`)
	s = strings.ReplaceAll(s, `#/$defs/`, `#/definitions/`)
	for from, to := range names {
		s = strings.ReplaceAll(s, `"`+from+`":`, `"`+to+`":`)
		s = strings.ReplaceAll(s, `/definitions/`+from+`"`, `/definitions/`+to+`"`)
	}
	var out bytes.Buffer
	if err := json.Indent(&out, []byte(s), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// durationSchema is the shape of config.Duration: a Go duration string such as "15m".
func durationSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:        "string",
		Pattern:     `^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$`,
		Description: `A Go duration, for example "2s", "15m" or "1h30m".`,
		Examples:    []any{"2s", "15m"},
	}
}

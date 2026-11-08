package typegen

import "github.com/google/jsonschema-go/jsonschema"

// objectBranches expands object-level alternatives without flattening their
// required keys into an intersection. Each result is one complete candidate,
// including its enclosing properties and allOf constraints. Property schemas
// themselves stay intact, so scalar/raw unions retain their own decoders.
func objectBranches(defs map[string]*jsonschema.Schema, schema *jsonschema.Schema) []*jsonschema.Schema {
	base := *schema
	base.Ref, base.AllOf, base.AnyOf, base.OneOf = "", nil, nil, nil
	candidates := []*jsonschema.Schema{&base}
	combine := func(alternatives []*jsonschema.Schema) {
		var combined []*jsonschema.Schema
		for _, candidate := range candidates {
			for _, alternative := range alternatives {
				merged := *candidate
				merged.Properties = mergedProperties(candidate.Properties, alternative.Properties)
				merged.Required = mergedRequired(candidate.Required, alternative.Required)
				if alternative.AdditionalProperties != nil {
					merged.AdditionalProperties = alternative.AdditionalProperties
				}
				if alternative.UnevaluatedProperties != nil {
					merged.UnevaluatedProperties = alternative.UnevaluatedProperties
				}
				combined = append(combined, &merged)
			}
		}
		candidates = combined
	}
	if schema.Ref != "" {
		def := defs[refName(schema.Ref)]
		if def == nil {
			panic("unresolved object branch ref " + schema.Ref)
		}
		combine(objectBranches(defs, def))
	}
	for _, branch := range schema.AllOf {
		combine(objectBranches(defs, branch))
	}
	for _, branches := range [][]*jsonschema.Schema{schema.AnyOf, schema.OneOf} {
		if len(branches) == 0 {
			continue
		}
		var alternatives []*jsonschema.Schema
		for _, branch := range branches {
			alternatives = append(alternatives, objectBranches(defs, branch)...)
		}
		combine(alternatives)
	}
	return candidates
}

// nestedRequiredFields identifies flattened scalars whose presence is selected
// by a nested alternative. Their zero values otherwise disappear via omitempty.
func nestedRequiredFields(defs map[string]*jsonschema.Schema, schema *jsonschema.Schema, fields map[string]unionField) []unionField {
	selected := map[string]unionField{}
	for _, branch := range unionBranches(schema) {
		common := mergedRequired(schema.Required, variantRequired(defs, branch))
		for _, alternative := range selectedObjectBranches(defs, schema, branch) {
			for _, name := range alternative.Required {
				field, ok := fields[name]
				if !ok || contains(common, name) {
					continue
				}
				switch schemaKind(defs, field.schema) {
				case "string", "integer", "number", "boolean":
					selected[name] = field
				}
			}
		}
	}
	var result []unionField
	for _, name := range sortedPropertyNamesFromUnion(selected) {
		result = append(result, selected[name])
	}
	return result
}

func selectedObjectBranches(defs map[string]*jsonschema.Schema, parent, branch *jsonschema.Schema) []*jsonschema.Schema {
	common := *parent
	common.AnyOf, common.OneOf = nil, nil
	return objectBranches(defs, &jsonschema.Schema{AllOf: []*jsonschema.Schema{&common, branch}})
}

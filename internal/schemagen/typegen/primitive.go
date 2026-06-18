// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package typegen

import (
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/google/jsonschema-go/jsonschema"
)

func schemaType(defs map[string]*jsonschema.Schema, schema *jsonschema.Schema, optional bool) (jen.Code, string) {
	if schema == nil {
		return jen.Any(), "any"
	}
	if schema.Ref != "" {
		return refType(defs, schema.Ref, optional)
	}
	if len(schema.AllOf) == 1 && schema.AllOf[0].Ref != "" {
		return refType(defs, schema.AllOf[0].Ref, optional)
	}
	if nonNull, nullable := nullableSchema(schema); nullable {
		base, text := schemaType(defs, nonNull, false)
		if text == "any" || text == "map[string]any" {
			return base, text
		}
		return jen.Op("*").Add(base), "*" + strings.TrimPrefix(text, "*")
	}
	if len(schema.AnyOf) > 1 || len(schema.OneOf) > 1 {
		return jen.Qual("encoding/json", "RawMessage"), "json.RawMessage"
	}
	if schema.Items != nil && schemaTypeName(schema) == "array" {
		item, text := schemaType(defs, schema.Items, false)
		return jen.Index().Add(item), "[]" + text
	}
	if schemaTypeName(schema) == "object" && schema.AdditionalProperties != nil {
		value, text := schemaType(defs, schema.AdditionalProperties, false)
		return jen.Map(jen.String()).Add(value), "map[string]" + text
	}

	switch schemaTypeName(schema) {
	case "string":
		return jen.String(), "string"
	case "integer":
		return integerType(schema.Format)
	case "number":
		return jen.Float64(), "float64"
	case "boolean":
		return jen.Bool(), "bool"
	case "object":
		return jen.Map(jen.String()).Any(), "map[string]any"
	case "array":
		return jen.Index().Any(), "[]any"
	case "null", "":
		return jen.Any(), "any"
	default:
		return jen.Any(), "any"
	}
}

func integerType(format string) (jen.Code, string) {
	switch format {
	case "int32":
		return jen.Id("int32"), "int32"
	case "uint16":
		return jen.Id("uint16"), "uint16"
	case "uint32":
		return jen.Id("uint32"), "uint32"
	case "uint64":
		return jen.Id("uint64"), "uint64"
	default:
		return jen.Int64(), "int64"
	}
}

func schemaDefaultTrue(schema *jsonschema.Schema) bool {
	return schema != nil && strings.TrimSpace(string(schema.Default)) == "true"
}

func refType(defs map[string]*jsonschema.Schema, ref string, optional bool) (jen.Code, string) {
	rawName := refName(ref)
	name := goDefinitionName(defs, rawName)
	if !optional || rawName == "" {
		return jen.Id(name), name
	}
	def := defs[rawName]
	if def == nil {
		return jen.Id(name), name
	}
	// Optional references to object definitions are pointers so callers can tell
	// absent from present-but-empty. Aliases and enums keep value types because
	// their zero values already match existing fixture expectations.
	if isObjectSchema(def) || isDiscriminatorUnion(defs, def) {
		return jen.Op("*").Id(name), "*" + name
	}
	return jen.Id(name), name
}

func nullableSchema(schema *jsonschema.Schema) (*jsonschema.Schema, bool) {
	if len(schema.Types) == 2 && contains(schema.Types, "null") {
		clone := *schema
		clone.Types = nil
		for _, typ := range schema.Types {
			if typ != "null" {
				clone.Type = typ
			}
		}
		return &clone, true
	}
	if len(schema.AnyOf) == 2 {
		var nonNull *jsonschema.Schema
		hasNull := false
		for _, branch := range schema.AnyOf {
			if schemaTypeName(branch) == "null" {
				hasNull = true
				continue
			}
			nonNull = branch
		}
		if hasNull && nonNull != nil {
			return nonNull, true
		}
	}
	return nil, false
}

func schemaTypeName(schema *jsonschema.Schema) string {
	if schema == nil {
		return ""
	}
	if schema.Type != "" {
		return schema.Type
	}
	if len(schema.Types) == 1 {
		return schema.Types[0]
	}
	return ""
}

func refName(ref string) string {
	return strings.TrimPrefix(ref, "#/$defs/")
}

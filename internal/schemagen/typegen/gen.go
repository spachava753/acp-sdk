// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package typegen

import (
	"bytes"
	"go/format"
	"sort"
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/google/jsonschema-go/jsonschema"
)

// Generate returns the contents of types_gen.go for schema.
func Generate(schema *jsonschema.Schema) []byte {
	if schema == nil || len(schema.Defs) == 0 || hasOnlyProtocolEnvelope(schema) {
		return nil
	}

	file := jen.NewFile("acp")
	var definitions []jen.Code
	var trailing []jen.Code
	needsMeta := false

	ignoredProtocolRefs := ignoredTopLevelProtocolRefs(schema)
	publicRefs := publicDefinitionRefs(schema, ignoredProtocolRefs)
	goNames := goDefinitionNames(schema.Defs)
	for _, name := range sortedDefNames(schema.Defs) {
		def := schema.Defs[name]
		if ignoredProtocolRefs[name] || (docsIgnored(def) && !publicRefs[name]) {
			continue
		}
		codes, meta, tail := definition(schema.Defs, goNames[name], def)
		definitions = append(definitions, codes...)
		trailing = append(trailing, tail...)
		needsMeta = needsMeta || meta
	}
	if len(definitions) == 0 && !needsMeta {
		return nil
	}

	if needsMeta {
		file.Add(commented("Meta", "Reserved metadata for protocol extensions.", jen.Type().Id("Meta").Map(jen.String()).Any()))
		file.Line()
	}
	for _, code := range definitions {
		file.Add(code)
	}
	for _, code := range trailing {
		file.Add(code)
	}

	var out bytes.Buffer
	if err := file.Render(&out); err != nil {
		panic(err)
	}
	formatted, err := format.Source(out.Bytes())
	if err != nil {
		panic(err)
	}
	return formatted
}

func definition(defs map[string]*jsonschema.Schema, name string, schema *jsonschema.Schema) ([]jen.Code, bool, []jen.Code) {
	if schema == nil {
		return nil, false, nil
	}

	switch {
	case isDiscriminatorUnion(defs, schema):
		codes, meta := discriminatorUnionCode(defs, name, schema)
		return codes, meta, nil
	case isArrayUnion(schema):
		return arrayUnionCode(defs, name, schema), false, nil
	case isObjectSchema(schema):
		return objectCode(defs, name, schema)
	case isStringEnum(schema):
		return stringEnumCode(name, schema), false, nil
	case isStringConstEnum(schema):
		return stringConstEnumCode(name, schema), false, nil
	case isPrimitiveConstUnion(schema):
		return primitiveConstUnionCode(name, schema), false, nil
	default:
		typ, text := schemaType(defs, schema, false)
		decl := jen.Type().Id(name)
		if text == "json.RawMessage" {
			decl.Op("=")
		}
		decl.Add(typ)
		return []jen.Code{commented(name, schema.Description, decl), jen.Line()}, false, nil
	}
}

func commented(name, description string, code jen.Code) jen.Code {
	if description == "" {
		return code
	}
	return commentStatement(name, description).Add(code)
}

func functionCommented(name, prefix, description string, code jen.Code) jen.Code {
	if description == "" {
		return jen.Commentf("%s %s", name, strings.TrimSpace(prefix)).Line().Add(code)
	}
	lines := strings.Split(description, "\n")
	stmt := jen.Empty()
	for i, line := range lines {
		if line == "" {
			stmt.Comment("").Line()
			continue
		}
		if i == 0 {
			stmt.Commentf("%s %s%s", name, prefix, line).Line()
		} else {
			stmt.Comment(line).Line()
		}
	}
	stmt.Add(code)
	return stmt
}

func commentStatement(name, description string) *jen.Statement {
	stmt := jen.Empty()
	for i, line := range strings.Split(description, "\n") {
		if line == "" {
			stmt.Comment("").Line()
			continue
		}
		if i == 0 && name != "" {
			stmt.Commentf("%s: %s", name, line).Line()
		} else {
			stmt.Comment(line).Line()
		}
	}
	return stmt
}

func sortedDefNames(defs map[string]*jsonschema.Schema) []string {
	names := make([]string, 0, len(defs))
	for name := range defs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedPropertyNames(properties map[string]*jsonschema.Schema) []string {
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func hasOnlyProtocolEnvelope(schema *jsonschema.Schema) bool {
	if len(schema.AnyOf) == 0 {
		return false
	}
	for _, branch := range schema.AnyOf {
		if branch.Title != "ProtocolLevel" {
			return false
		}
	}
	return true
}

func docsIgnored(schema *jsonschema.Schema) bool {
	if schema == nil || schema.Extra == nil {
		return false
	}
	ignored, _ := schema.Extra["x-docs-ignore"].(bool)
	return ignored
}

func publicDefinitionRefs(schema *jsonschema.Schema, ignoredProtocolRefs map[string]bool) map[string]bool {
	refs := map[string]bool{}
	if schema == nil {
		return refs
	}
	for name, def := range schema.Defs {
		if docsIgnored(def) || ignoredProtocolRefs[name] {
			continue
		}
		collectRefs(def, schema.Defs, refs, map[string]bool{})
	}
	return refs
}

func ignoredTopLevelProtocolRefs(schema *jsonschema.Schema) map[string]bool {
	ignored := map[string]bool{}
	public := map[string]bool{}
	if schema == nil {
		return ignored
	}
	for _, branch := range schema.AnyOf {
		if branch.Title == "ProtocolLevel" && docsIgnored(branch) {
			collectRefs(branch, schema.Defs, ignored, map[string]bool{})
			continue
		}
		collectRefs(branch, schema.Defs, public, map[string]bool{})
	}
	for name, def := range schema.Defs {
		if docsIgnored(def) || ignored[name] {
			continue
		}
		collectRefs(def, schema.Defs, public, map[string]bool{})
	}
	for ref := range public {
		delete(ignored, ref)
	}
	return ignored
}

func collectRefs(schema *jsonschema.Schema, defs map[string]*jsonschema.Schema, refs map[string]bool, seen map[string]bool) {
	if schema == nil {
		return
	}
	if schema.Ref != "" {
		name := refName(schema.Ref)
		refs[name] = true
		if !seen[name] {
			seen[name] = true
			collectRefs(defs[name], defs, refs, seen)
		}
	}
	for _, branch := range schema.AnyOf {
		collectRefs(branch, defs, refs, seen)
	}
	for _, branch := range schema.OneOf {
		collectRefs(branch, defs, refs, seen)
	}
	for _, branch := range schema.AllOf {
		collectRefs(branch, defs, refs, seen)
	}
	for _, prop := range schema.Properties {
		collectRefs(prop, defs, refs, seen)
	}
	collectRefs(schema.Items, defs, refs, seen)
	collectRefs(schema.AdditionalProperties, defs, refs, seen)
}

func contains[S ~[]E, E comparable](slice S, value E) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}

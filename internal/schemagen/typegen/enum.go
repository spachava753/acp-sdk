// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package typegen

import (
	"github.com/dave/jennifer/jen"
	"github.com/google/jsonschema-go/jsonschema"
)

func stringEnumCode(name string, schema *jsonschema.Schema) []jen.Code {
	var values []jen.Code
	usedNames := map[string]bool{}
	for _, value := range schema.Enum {
		text, ok := value.(string)
		if !ok {
			continue
		}
		constName := uniqueConstName(name+constValueName(text), usedNames)
		values = append(values, jen.Id(constName).Id(name).Op("=").Lit(text))
	}
	return []jen.Code{
		commented(name, schema.Description, jen.Type().Id(name).String()),
		jen.Line(),
		jen.Const().Defs(values...),
		jen.Line(),
	}
}

func isStringEnum(schema *jsonschema.Schema) bool {
	if schemaTypeName(schema) != "string" || len(schema.Enum) == 0 {
		return false
	}
	for _, value := range schema.Enum {
		if _, ok := value.(string); !ok {
			return false
		}
	}
	return true
}

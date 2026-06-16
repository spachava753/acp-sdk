// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package typegen

import (
	"strings"
	"unicode"

	"github.com/dave/jennifer/jen"
	"github.com/google/jsonschema-go/jsonschema"
)

func stringConstEnumCode(name string, schema *jsonschema.Schema) []jen.Code {
	var values []jen.Code
	branches := schema.OneOf
	if len(branches) == 0 {
		branches = schema.AnyOf
	}
	for _, branch := range branches {
		text, _ := constString(branch)
		constName := name + pascalIdentifier(text)
		values = append(values, commented(constName, branch.Description, jen.Id(constName).Id(name).Op("=").Lit(text)))
	}
	return []jen.Code{
		commented(name, schema.Description, jen.Type().Id(name).String()),
		jen.Line(),
		jen.Const().Defs(values...),
		jen.Line(),
	}
}

func isStringConstEnum(schema *jsonschema.Schema) bool {
	branches := schema.OneOf
	if len(branches) == 0 {
		branches = schema.AnyOf
	}
	if len(branches) == 0 {
		return false
	}
	for _, branch := range branches {
		if schemaTypeName(branch) != "string" {
			return false
		}
		if _, ok := constString(branch); !ok {
			return false
		}
	}
	return true
}

func constString(schema *jsonschema.Schema) (string, bool) {
	if schema == nil || schema.Const == nil {
		return "", false
	}
	text, ok := (*schema.Const).(string)
	return text, ok
}

func jsonTag(name string, omitempty, omitzero bool) string {
	parts := []string{name}
	if omitempty {
		parts = append(parts, "omitempty")
	}
	if omitzero {
		parts = append(parts, "omitzero")
	}
	return strings.Join(parts, ",")
}

func fieldName(jsonName string) string {
	if jsonName == "_meta" {
		return "Meta"
	}
	if jsonName == "id" {
		return "ID"
	}
	if jsonName == "uri" {
		return "URI"
	}
	name := pascalIdentifier(jsonName)
	if strings.HasSuffix(name, "Id") {
		name = strings.TrimSuffix(name, "Id") + "ID"
	}
	return name
}

func pascalIdentifier(value string) string {
	var words []string
	var word strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word.WriteRune(r)
			continue
		}
		if word.Len() > 0 {
			words = append(words, word.String())
			word.Reset()
		}
	}
	if word.Len() > 0 {
		words = append(words, word.String())
	}
	if len(words) == 0 {
		return ""
	}
	for i, word := range words {
		words[i] = upperFirst(word)
	}
	return strings.Join(words, "")
}

func upperFirst(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func lowerCamel(value string) string {
	if value == "" {
		return ""
	}
	if value == strings.ToUpper(value) {
		return strings.ToLower(value)
	}
	runes := []rune(value)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

func parameterName(jsonName string) string {
	name := lowerCamel(fieldName(jsonName))
	if goKeywords[name] {
		return name + "_"
	}
	return name
}

var goKeywords = map[string]bool{
	"break": true, "default": true, "func": true, "interface": true, "select": true,
	"case": true, "defer": true, "go": true, "map": true, "struct": true,
	"chan": true, "else": true, "goto": true, "package": true, "switch": true,
	"const": true, "fallthrough": true, "if": true, "range": true, "type": true,
	"continue": true, "for": true, "import": true, "return": true, "var": true,
}

func lowerFirstSentence(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

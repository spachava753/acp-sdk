// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package typegen

import (
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/google/jsonschema-go/jsonschema"
)

type deserializeField struct {
	jsonName         string
	goName           string
	typeCode         jen.Code
	typeText         string
	itemTypeCode     jen.Code
	enumValues       []string
	defaultOnError   bool
	skipInvalidItems bool
}

func newDeserializeField(defs map[string]*jsonschema.Schema, jsonName string, prop *jsonschema.Schema, typeCode jen.Code, typeText string) deserializeField {
	field := deserializeField{
		jsonName:         jsonName,
		goName:           fieldName(jsonName),
		typeCode:         typeCode,
		typeText:         typeText,
		enumValues:       stringEnumValues(defs, prop),
		defaultOnError:   schemaBoolExtra(prop, "x-deserialize-default-on-error"),
		skipInvalidItems: schemaBoolExtra(prop, "x-deserialize-skip-invalid-items"),
	}
	if _, ok := skipInvalidItemsTarget(field); ok {
		if itemSchema := deserializeItemSchema(prop); itemSchema != nil {
			field.itemTypeCode, _ = schemaType(defs, itemSchema, false)
		}
	}
	return field
}

func deserializeItemSchema(schema *jsonschema.Schema) *jsonschema.Schema {
	if schema == nil {
		return nil
	}
	if nonNull, nullable := nullableSchema(schema); nullable {
		return deserializeItemSchema(nonNull)
	}
	if schemaTypeName(schema) == "array" {
		return schema.Items
	}
	return nil
}

func stringEnumValues(defs map[string]*jsonschema.Schema, schema *jsonschema.Schema) []string {
	if schema == nil {
		return nil
	}
	if schema.Ref != "" {
		return stringEnumValues(defs, defs[refName(schema.Ref)])
	}
	if len(schema.AllOf) == 1 && schema.AllOf[0].Ref != "" {
		return stringEnumValues(defs, schema.AllOf[0])
	}
	if nonNull, nullable := nullableSchema(schema); nullable {
		return stringEnumValues(defs, nonNull)
	}
	if isStringEnum(schema) {
		values := make([]string, 0, len(schema.Enum))
		for _, value := range schema.Enum {
			values = append(values, value.(string))
		}
		return values
	}
	if text, ok := constString(schema); ok && schemaTypeName(schema) == "string" {
		return []string{text}
	}
	branches := unionBranches(schema)
	if len(branches) == 0 {
		return nil
	}
	var values []string
	for _, branch := range branches {
		if schemaTypeName(branch) == "null" {
			continue
		}
		branchValues := stringEnumValues(defs, branch)
		if len(branchValues) == 0 {
			return nil
		}
		values = append(values, branchValues...)
	}
	return values
}

func schemaBoolExtra(schema *jsonschema.Schema, key string) bool {
	if schema == nil || schema.Extra == nil {
		return false
	}
	value, _ := schema.Extra[key].(bool)
	return value
}

func mergeDeserializeRules(existing, field deserializeField) deserializeField {
	existing.defaultOnError = existing.defaultOnError || field.defaultOnError
	existing.skipInvalidItems = existing.skipInvalidItems || field.skipInvalidItems
	for _, value := range field.enumValues {
		if !contains(existing.enumValues, value) {
			existing.enumValues = append(existing.enumValues, value)
		}
	}
	if existing.itemTypeCode == nil {
		existing.itemTypeCode = field.itemTypeCode
	}
	return existing
}

func needsDeserializeUnmarshal(field deserializeField) bool {
	return field.defaultOnError || field.skipInvalidItems
}

func deserializeUnmarshalCode(name string, fields []deserializeField) (jen.Code, bool) {
	var tolerant []deserializeField
	for _, field := range fields {
		if needsDeserializeUnmarshal(field) {
			tolerant = append(tolerant, field)
		}
	}
	if len(tolerant) == 0 {
		return nil, false
	}

	receiver := receiverName(name)
	var rawFields []jen.Code
	for _, field := range tolerant {
		rawFields = append(rawFields, jen.Id(field.goName).Qual("encoding/json", "RawMessage").Tag(map[string]string{"json": field.jsonName}))
	}
	rawFields = append(rawFields, jen.Op("*").Id("alias"))

	body := []jen.Code{
		jen.Type().Id("alias").Id(name),
		jen.Id("decoded").Op(":=").Id("alias").Values(),
		jen.Id("raw").Op(":=").Struct(rawFields...).Values(jen.Id("alias").Op(":").Op("&").Id("decoded")),
		jen.If(jen.Err().Op(":=").Qual("encoding/json", "Unmarshal").Call(jen.Id("data"), jen.Op("&").Id("raw")), jen.Err().Op("!=").Nil()).Block(jen.Return(jen.Err())),
	}
	for _, field := range tolerant {
		body = append(body, deserializeFieldUnmarshalCode(field))
	}
	body = append(body,
		jen.Op("*").Id(receiver).Op("=").Id(name).Call(jen.Id("decoded")),
		jen.Return(jen.Nil()),
	)
	return jen.Comment("UnmarshalJSON implements json.Unmarshaler.").Line().Func().Params(jen.Id(receiver).Op("*").Id(name)).Id("UnmarshalJSON").Params(jen.Id("data").Index().Byte()).Error().Block(body...), true
}

func deserializeFieldUnmarshalCode(field deserializeField) jen.Code {
	if field.skipInvalidItems && field.itemTypeCode != nil {
		if pointer, ok := skipInvalidItemsTarget(field); ok {
			return jen.If(jen.Len(jen.Id("raw").Dot(field.goName)).Op(">").Lit(0)).Block(skipInvalidItemsCode(field, pointer)...)
		}
	}
	if field.defaultOnError && len(field.enumValues) > 0 {
		return jen.If(jen.Len(jen.Id("raw").Dot(field.goName)).Op(">").Lit(0)).Block(defaultEnumFieldCode(field)...)
	}
	if field.defaultOnError {
		return jen.If(jen.Len(jen.Id("raw").Dot(field.goName)).Op(">").Lit(0)).Block(
			jen.Id("_").Op("=").Qual("encoding/json", "Unmarshal").Call(jen.Id("raw").Dot(field.goName), jen.Op("&").Id("decoded").Dot(field.goName)),
		)
	}
	return jen.If(jen.Len(jen.Id("raw").Dot(field.goName)).Op(">").Lit(0)).Block(
		jen.If(jen.Err().Op(":=").Qual("encoding/json", "Unmarshal").Call(jen.Id("raw").Dot(field.goName), jen.Op("&").Id("decoded").Dot(field.goName)), jen.Err().Op("!=").Nil()).Block(jen.Return(jen.Err())),
	)
}

func defaultEnumFieldCode(field deserializeField) []jen.Code {
	typeName := strings.TrimPrefix(field.typeText, "*")
	var cases []jen.Code
	for _, value := range field.enumValues {
		cases = append(cases, jen.Lit(value))
	}
	assignment := jen.Id("decoded").Dot(field.goName).Op("=").Id("value")
	if strings.HasPrefix(field.typeText, "*") {
		assignment = jen.Id("decoded").Dot(field.goName).Op("=").Op("&").Id("value")
	}
	return []jen.Code{
		jen.Var().Id("value").Id(typeName),
		jen.If(jen.Err().Op(":=").Qual("encoding/json", "Unmarshal").Call(jen.Id("raw").Dot(field.goName), jen.Op("&").Id("value")), jen.Err().Op("==").Nil()).Block(
			jen.Switch(jen.String().Call(jen.Id("value"))).Block(
				jen.Case(cases...).Block(assignment),
			),
		),
	}
}

func skipInvalidItemsTarget(field deserializeField) (bool, bool) {
	if strings.HasPrefix(field.typeText, "[]") {
		return false, true
	}
	if strings.HasPrefix(field.typeText, "*[]") {
		return true, true
	}
	return false, false
}

func skipInvalidItemsCode(field deserializeField, pointer bool) []jen.Code {
	decodeValues := skipInvalidItemsDecodeValues(field, pointer)
	if field.defaultOnError {
		condition := jen.Err().Op(":=").Qual("encoding/json", "Unmarshal").Call(jen.Id("raw").Dot(field.goName), jen.Op("&").Id("values"))
		if pointer {
			return []jen.Code{
				jen.Var().Id("values").Index().Qual("encoding/json", "RawMessage"),
				jen.If(condition, jen.Err().Op("==").Nil().Op("&&").Id("values").Op("!=").Nil()).Block(decodeValues...),
			}
		}
		return []jen.Code{
			jen.Var().Id("values").Index().Qual("encoding/json", "RawMessage"),
			jen.If(condition, jen.Err().Op("==").Nil()).Block(decodeValues...),
		}
	}
	if pointer {
		return []jen.Code{
			jen.Var().Id("values").Index().Qual("encoding/json", "RawMessage"),
			jen.If(jen.Err().Op(":=").Qual("encoding/json", "Unmarshal").Call(jen.Id("raw").Dot(field.goName), jen.Op("&").Id("values")), jen.Err().Op("!=").Nil()).Block(jen.Return(jen.Err())).Else().If(jen.Id("values").Op("!=").Nil()).Block(decodeValues...),
		}
	}
	return []jen.Code{
		jen.Var().Id("values").Index().Qual("encoding/json", "RawMessage"),
		jen.If(jen.Err().Op(":=").Qual("encoding/json", "Unmarshal").Call(jen.Id("raw").Dot(field.goName), jen.Op("&").Id("values")), jen.Err().Op("!=").Nil()).Block(jen.Return(jen.Err())).Else().Block(decodeValues...),
	}
}

func skipInvalidItemsDecodeValues(field deserializeField, pointer bool) []jen.Code {
	if pointer {
		return []jen.Code{
			jen.Id("items").Op(":=").Index().Add(field.itemTypeCode).Values(),
			jen.For(jen.List(jen.Id("_"), jen.Id("value")).Op(":=").Range().Id("values")).Block(
				jen.Var().Id("item").Add(field.itemTypeCode),
				jen.If(jen.Err().Op(":=").Qual("encoding/json", "Unmarshal").Call(jen.Id("value"), jen.Op("&").Id("item")), jen.Err().Op("==").Nil()).Block(
					jen.Id("items").Op("=").Append(jen.Id("items"), jen.Id("item")),
				),
			),
			jen.Id("decoded").Dot(field.goName).Op("=").Op("&").Id("items"),
		}
	}
	return []jen.Code{
		jen.Id("decoded").Dot(field.goName).Op("=").Add(field.typeCode).Values(),
		jen.For(jen.List(jen.Id("_"), jen.Id("value")).Op(":=").Range().Id("values")).Block(
			jen.Var().Id("item").Add(field.itemTypeCode),
			jen.If(jen.Err().Op(":=").Qual("encoding/json", "Unmarshal").Call(jen.Id("value"), jen.Op("&").Id("item")), jen.Err().Op("==").Nil()).Block(
				jen.Id("decoded").Dot(field.goName).Op("=").Append(jen.Id("decoded").Dot(field.goName), jen.Id("item")),
			),
		),
	}
}

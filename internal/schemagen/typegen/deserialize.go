// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package typegen

import (
	"fmt"
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
	itemValidator    *skipInvalidItemValidator
	enumValues       []string
	defaultOnError   bool
	skipInvalidItems bool
}

type skipInvalidItemValidator struct {
	discriminator string
	cases         []jen.Code
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
	if field.skipInvalidItems {
		if _, ok := skipInvalidItemsTarget(field); !ok {
			panic(fmt.Sprintf("x-deserialize-skip-invalid-items is only supported for array fields: %s", jsonName))
		}
		itemSchema := deserializeItemSchema(prop)
		if itemSchema == nil {
			panic(fmt.Sprintf("x-deserialize-skip-invalid-items field %s has no array item schema", jsonName))
		}
		field.itemTypeCode, _ = schemaType(defs, itemSchema, false)
		field.itemValidator = skipInvalidItemsValidator(defs, itemSchema)
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

func skipInvalidItemsValidator(defs map[string]*jsonschema.Schema, schema *jsonschema.Schema) *skipInvalidItemValidator {
	if validator := discriminatorSkipInvalidItemsValidator(defs, schema); validator != nil {
		return validator
	}
	if cases := stringConstEnumItemCases(defs, schema); len(cases) > 0 {
		return &skipInvalidItemValidator{cases: cases}
	}
	if cases := stringEnumItemCases(defs, schema); len(cases) > 0 {
		return &skipInvalidItemValidator{cases: cases}
	}
	return nil
}

func discriminatorSkipInvalidItemsValidator(defs map[string]*jsonschema.Schema, schema *jsonschema.Schema) *skipInvalidItemValidator {
	name := referencedDefinitionName(schema)
	if name == "" {
		return nil
	}
	def := defs[name]
	if def == nil || !isDiscriminatorUnion(defs, def) {
		return nil
	}
	branches := unionBranches(def)
	discriminator := discriminatorField(defs, def, branches)
	if discriminator == "" {
		return nil
	}
	discriminatorType := discriminatorTypeName(defs, goDefinitionName(defs, name))
	constNames := discriminatorConstNames(defs, discriminatorType, discriminator, branches)
	seen := map[string]bool{}
	var cases []jen.Code
	for _, branch := range branches {
		value, _ := variantConst(defs, branch, discriminator)
		if seen[value] {
			continue
		}
		seen[value] = true
		if constName := constNames[value]; constName != "" {
			cases = append(cases, jen.Id(constName))
			continue
		}
		if value == "" {
			cases = append(cases, jen.Lit(""))
		}
	}
	if len(cases) == 0 {
		return nil
	}
	return &skipInvalidItemValidator{discriminator: fieldName(discriminator), cases: cases}
}

func stringConstEnumItemCases(defs map[string]*jsonschema.Schema, schema *jsonschema.Schema) []jen.Code {
	def, typeName, ok := stringConstEnumItemSchema(defs, schema)
	if !ok {
		return nil
	}
	used := map[string]bool{}
	var cases []jen.Code
	for _, branch := range unionBranches(def) {
		text, ok := constString(branch)
		if !ok || schemaTypeName(branch) != "string" {
			return nil
		}
		if typeName == "" {
			cases = append(cases, jen.Lit(text))
			continue
		}
		cases = append(cases, jen.Id(uniqueConstName(typeName+primitiveConstName(branch), used)))
	}
	return cases
}

func stringEnumItemCases(defs map[string]*jsonschema.Schema, schema *jsonschema.Schema) []jen.Code {
	values := stringEnumValues(defs, schema)
	if len(values) == 0 {
		return nil
	}
	cases := make([]jen.Code, 0, len(values))
	for _, value := range values {
		cases = append(cases, jen.Lit(value))
	}
	return cases
}

func stringConstEnumItemSchema(defs map[string]*jsonschema.Schema, schema *jsonschema.Schema) (*jsonschema.Schema, string, bool) {
	if schema == nil {
		return nil, "", false
	}
	if schema.Ref != "" {
		name := refName(schema.Ref)
		def := defs[name]
		resolved, _, ok := stringConstEnumItemSchema(defs, def)
		return resolved, goDefinitionName(defs, name), ok
	}
	if len(schema.AllOf) == 1 && schema.AllOf[0].Ref != "" {
		return stringConstEnumItemSchema(defs, schema.AllOf[0])
	}
	if nonNull, nullable := nullableSchema(schema); nullable {
		return stringConstEnumItemSchema(defs, nonNull)
	}
	if !isStringConstEnum(schema) {
		return nil, "", false
	}
	return schema, "", true
}

func referencedDefinitionName(schema *jsonschema.Schema) string {
	if schema == nil {
		return ""
	}
	if schema.Ref != "" {
		return refName(schema.Ref)
	}
	if len(schema.AllOf) == 1 && schema.AllOf[0].Ref != "" {
		return refName(schema.AllOf[0].Ref)
	}
	return ""
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
	if existing.itemValidator == nil {
		existing.itemValidator = field.itemValidator
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
		if field.typeText == "any" {
			return jen.If(jen.Len(jen.Id("raw").Dot(field.goName)).Op(">").Lit(0)).Block(skipInvalidAnyItemsCode(field)...)
		}
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

func skipInvalidAnyItemsCode(field deserializeField) []jen.Code {
	fallback := []jen.Code{
		jen.Id("_").Op("=").Qual("encoding/json", "Unmarshal").Call(jen.Id("raw").Dot(field.goName), jen.Op("&").Id("decoded").Dot(field.goName)),
	}
	if !field.defaultOnError {
		fallback = []jen.Code{
			jen.If(jen.Err().Op(":=").Qual("encoding/json", "Unmarshal").Call(jen.Id("raw").Dot(field.goName), jen.Op("&").Id("decoded").Dot(field.goName)), jen.Err().Op("!=").Nil()).Block(jen.Return(jen.Err())),
		}
	}
	return []jen.Code{
		jen.Var().Id("values").Index().Qual("encoding/json", "RawMessage"),
		jen.If(jen.Err().Op(":=").Qual("encoding/json", "Unmarshal").Call(jen.Id("raw").Dot(field.goName), jen.Op("&").Id("values")), jen.Err().Op("==").Nil().Op("&&").Id("values").Op("!=").Nil()).Block(skipInvalidAnyItemsDecodeValues(field)...).Else().Block(fallback...),
	}
}

func skipInvalidAnyItemsDecodeValues(field deserializeField) []jen.Code {
	appendItem := jen.Id("items").Op("=").Append(jen.Id("items"), jen.Id("item"))
	return []jen.Code{
		jen.Id("items").Op(":=").Index().Add(field.itemTypeCode).Values(),
		jen.For(jen.List(jen.Id("_"), jen.Id("value")).Op(":=").Range().Id("values")).Block(
			jen.Var().Id("item").Add(field.itemTypeCode),
			skipInvalidItemDecodeCode(field, appendItem),
		),
		jen.Id("decoded").Dot(field.goName).Op("=").Id("items"),
	}
}

func skipInvalidItemsDecodeValues(field deserializeField, pointer bool) []jen.Code {
	if pointer {
		appendItem := jen.Id("items").Op("=").Append(jen.Id("items"), jen.Id("item"))
		return []jen.Code{
			jen.Id("items").Op(":=").Index().Add(field.itemTypeCode).Values(),
			jen.For(jen.List(jen.Id("_"), jen.Id("value")).Op(":=").Range().Id("values")).Block(
				jen.Var().Id("item").Add(field.itemTypeCode),
				skipInvalidItemDecodeCode(field, appendItem),
			),
			jen.Id("decoded").Dot(field.goName).Op("=").Op("&").Id("items"),
		}
	}
	appendItem := jen.Id("decoded").Dot(field.goName).Op("=").Append(jen.Id("decoded").Dot(field.goName), jen.Id("item"))
	return []jen.Code{
		jen.Id("decoded").Dot(field.goName).Op("=").Add(field.typeCode).Values(),
		jen.For(jen.List(jen.Id("_"), jen.Id("value")).Op(":=").Range().Id("values")).Block(
			jen.Var().Id("item").Add(field.itemTypeCode),
			skipInvalidItemDecodeCode(field, appendItem),
		),
	}
}

func skipInvalidItemDecodeCode(field deserializeField, appendItem jen.Code) jen.Code {
	body := []jen.Code{appendItem}
	if field.itemValidator != nil {
		switchExpr := jen.Id("item")
		if field.itemValidator.discriminator != "" {
			switchExpr = jen.Id("item").Dot(field.itemValidator.discriminator)
		}
		body = []jen.Code{
			jen.Switch(switchExpr).Block(
				jen.Case(field.itemValidator.cases...).Block(appendItem),
			),
		}
	}
	return jen.If(jen.Err().Op(":=").Qual("encoding/json", "Unmarshal").Call(jen.Id("value"), jen.Op("&").Id("item")), jen.Err().Op("==").Nil()).Block(body...)
}

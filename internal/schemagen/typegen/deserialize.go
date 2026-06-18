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
	defaultOnError   bool
	skipInvalidItems bool
}

func newDeserializeField(defs map[string]*jsonschema.Schema, jsonName string, prop *jsonschema.Schema, typeCode jen.Code, typeText string) deserializeField {
	field := deserializeField{
		jsonName:         jsonName,
		goName:           fieldName(jsonName),
		typeCode:         typeCode,
		typeText:         typeText,
		defaultOnError:   schemaBoolExtra(prop, "x-deserialize-default-on-error"),
		skipInvalidItems: schemaBoolExtra(prop, "x-deserialize-skip-invalid-items"),
	}
	if strings.HasPrefix(typeText, "[]") && prop != nil && prop.Items != nil {
		field.itemTypeCode, _ = schemaType(defs, prop.Items, false)
	}
	return field
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
	if field.skipInvalidItems && strings.HasPrefix(field.typeText, "[]") && field.itemTypeCode != nil {
		return jen.If(jen.Len(jen.Id("raw").Dot(field.goName)).Op(">").Lit(0)).Block(skipInvalidItemsCode(field)...)
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

func skipInvalidItemsCode(field deserializeField) []jen.Code {
	decodeValues := []jen.Code{
		jen.Id("decoded").Dot(field.goName).Op("=").Add(field.typeCode).Values(),
		jen.For(jen.List(jen.Id("_"), jen.Id("value")).Op(":=").Range().Id("values")).Block(
			jen.Var().Id("item").Add(field.itemTypeCode),
			jen.If(jen.Err().Op(":=").Qual("encoding/json", "Unmarshal").Call(jen.Id("value"), jen.Op("&").Id("item")), jen.Err().Op("==").Nil()).Block(
				jen.Id("decoded").Dot(field.goName).Op("=").Append(jen.Id("decoded").Dot(field.goName), jen.Id("item")),
			),
		),
	}
	if field.defaultOnError {
		return []jen.Code{
			jen.Var().Id("values").Index().Qual("encoding/json", "RawMessage"),
			jen.If(jen.Err().Op(":=").Qual("encoding/json", "Unmarshal").Call(jen.Id("raw").Dot(field.goName), jen.Op("&").Id("values")), jen.Err().Op("==").Nil()).Block(decodeValues...),
		}
	}
	return []jen.Code{
		jen.Var().Id("values").Index().Qual("encoding/json", "RawMessage"),
		jen.If(jen.Err().Op(":=").Qual("encoding/json", "Unmarshal").Call(jen.Id("raw").Dot(field.goName), jen.Op("&").Id("values")), jen.Err().Op("!=").Nil()).Block(jen.Return(jen.Err())).Else().Block(decodeValues...),
	}
}

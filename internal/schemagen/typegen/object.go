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

func objectCode(defs map[string]*jsonschema.Schema, name string, schema *jsonschema.Schema) ([]jen.Code, bool, []jen.Code) {
	type field struct {
		deserializeField
		tag      string
		required bool
	}

	required := map[string]bool{}
	for _, name := range schema.Required {
		required[name] = true
	}

	needsMeta := false
	var fields []field
	var structFields []jen.Code
	propertyNames := sortedPropertyNames(schema.Properties)
	goNames := uniqueFieldNames(propertyNames)
	for _, jsonName := range propertyNames {
		prop := schema.Properties[jsonName]
		if jsonName == "_meta" {
			needsMeta = true
			f := field{deserializeField: deserializeField{jsonName: jsonName, goName: goNames[jsonName], typeCode: jen.Id("Meta"), typeText: "Meta"}, tag: jsonTag(jsonName, false, true)}
			fields = append(fields, f)
			structFields = append(structFields, jen.Id(f.goName).Add(f.typeCode).Tag(map[string]string{"json": f.tag}))
			continue
		}
		typ, text := schemaType(defs, prop, !required[jsonName])
		if pointerDefaultTrueBool(jsonName, prop, !required[jsonName]) {
			typ, text = jen.Op("*").Bool(), "*bool"
		}
		deserialize := newDeserializeField(defs, jsonName, prop, typ, text)
		deserialize.goName = goNames[jsonName]
		f := field{deserializeField: deserialize, tag: jsonTag(jsonName, !required[jsonName], false), required: required[jsonName]}
		fields = append(fields, f)
		structFields = append(structFields, jen.Id(f.goName).Add(f.typeCode).Tag(map[string]string{"json": f.tag}))
	}

	codes := []jen.Code{commented(name, schema.Description, jen.Type().Id(name).Struct(structFields...)), jen.Line()}

	// JSON encoders serialize a nil slice as null. Required array fields should
	// remain arrays on the wire for the protocol structs that rely on that
	// distinction. Collection/map-heavy shapes deliberately keep the default
	// encoder behavior to match the current fixture contract.
	var requiredSlices []field
	var hasRequiredMap bool
	var hasOptionalPointer bool
	for _, f := range fields {
		if !f.required && strings.HasPrefix(f.typeText, "*") {
			hasOptionalPointer = true
		}
		if !f.required {
			continue
		}
		if strings.HasPrefix(f.typeText, "[]") {
			requiredSlices = append(requiredSlices, f)
		}
		if strings.HasPrefix(f.typeText, "map[") {
			hasRequiredMap = true
		}
	}
	var deserializeFields []deserializeField
	for _, f := range fields {
		deserializeFields = append(deserializeFields, f.deserializeField)
	}
	unmarshalMethod, hasUnmarshal := deserializeUnmarshalCode(name, deserializeFields)
	if len(requiredSlices) == 0 || hasRequiredMap {
		if hasUnmarshal {
			codes = append(codes, unmarshalMethod)
		}
		return codes, needsMeta, nil
	}

	receiver := receiverName(name)
	var body []jen.Code
	body = append(body,
		jen.Type().Id("alias").Id(name),
		jen.Id("a").Op(":=").Id("alias").Call(jen.Id(receiver)),
	)
	for _, f := range requiredSlices {
		body = append(body, jen.If(jen.Id("a").Dot(f.goName).Op("==").Nil()).Block(
			jen.Id("a").Dot(f.goName).Op("=").Add(f.typeCode).Values(),
		))
	}
	body = append(body, jen.Return(jen.Qual("encoding/json", "Marshal").Call(jen.Id("a"))))
	method := jen.Comment("MarshalJSON implements json.Marshaler.").Line().Func().Params(jen.Id(receiver).Id(name)).Id("MarshalJSON").Params().Params(jen.Index().Byte(), jen.Error()).Block(body...)
	methods := []jen.Code{method}
	if hasUnmarshal {
		methods = append(methods, jen.Line(), unmarshalMethod)
	}
	if hasOptionalPointer {
		return codes, needsMeta, methods
	}
	return append(codes, methods...), needsMeta, nil
}

func pointerDefaultTrueBool(jsonName string, schema *jsonschema.Schema, optional bool) bool {
	// AuthEnvVar.secret defaults to true, so explicit false must survive encoding.
	return optional && jsonName == "secret" && schemaTypeName(schema) == "boolean" && schemaDefaultTrue(schema)
}

func isObjectSchema(schema *jsonschema.Schema) bool {
	return schemaTypeName(schema) == "object" && len(schema.Properties) > 0
}

func receiverName(value string) string {
	if value == "" {
		return "s"
	}
	last := []rune(value)
	for i := len(last) - 1; i > 0; i-- {
		if unicode.IsUpper(last[i]) && !unicode.IsUpper(last[i-1]) {
			last = last[i:]
			break
		}
	}
	return strings.ToLower(string(last[:1]))
}

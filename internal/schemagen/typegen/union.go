// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package typegen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/google/jsonschema-go/jsonschema"
)

type unionField struct {
	jsonName string
	goName   string
	typeCode jen.Code
	typeText string
	schema   *jsonschema.Schema
}

type unionRequiredSlice struct {
	discriminatorValue string
	field              unionField
}

func discriminatorUnionCode(defs map[string]*jsonschema.Schema, name string, schema *jsonschema.Schema) []jen.Code {
	branches := unionBranches(schema)
	discriminator := discriminatorField(defs, branches)
	requiredCount := map[string]int{}
	fieldOrder := []string{}

	fieldsByJSON := map[string]unionField{}
	var requiredSlices []unionRequiredSlice
	for _, branch := range branches {
		value, _ := variantConst(defs, branch)
		for _, req := range variantRequired(defs, branch) {
			requiredCount[req]++
		}
		for jsonName, prop := range variantProperties(defs, branch) {
			if contains(variantRequired(defs, branch), jsonName) {
				typ, text := schemaType(defs, prop, false)
				if strings.HasPrefix(text, "[]") {
					requiredSlices = append(requiredSlices, unionRequiredSlice{discriminatorValue: value, field: unionField{jsonName: jsonName, goName: fieldName(jsonName), typeCode: typ, typeText: text, schema: prop}})
				}
			}
			if prop.Const != nil || jsonName == discriminator {
				continue
			}
			if existing, ok := fieldsByJSON[jsonName]; ok {
				_, text := schemaType(defs, prop, false)
				if existing.typeText != text && schemaKind(defs, prop) != schemaKind(defs, existing.schema) {
					existing.typeCode = jen.Any()
					existing.typeText = "any"
				}
				fieldsByJSON[jsonName] = existing
				continue
			}
			typ, text := schemaType(defs, prop, false)
			fieldsByJSON[jsonName] = unionField{jsonName: jsonName, goName: fieldName(jsonName), typeCode: typ, typeText: text, schema: prop}
			fieldOrder = append(fieldOrder, jsonName)
		}
	}
	sort.Strings(fieldOrder)

	var structFields []jen.Code
	if discriminator != "" {
		structFields = append(structFields, jen.Id(fieldName(discriminator)).Id(name+"Type").Tag(map[string]string{"json": jsonTag(discriminator, requiredCount[discriminator] != len(branches), false)}))
	}
	for _, jsonName := range fieldOrder {
		field := fieldsByJSON[jsonName]
		requiredInAllVariants := requiredCount[jsonName] == len(branches)
		structFields = append(structFields, jen.Id(field.goName).Add(field.typeCode).Tag(map[string]string{"json": jsonTag(jsonName, !requiredInAllVariants, !requiredInAllVariants && needsOmitZero(defs, field.schema))}))
	}

	var codes []jen.Code
	codes = append(codes, commented(name, schema.Description, jen.Type().Id(name).Struct(structFields...)), jen.Line())
	if discriminator != "" {
		codes = append(codes, jen.Commentf("%sType is the discriminator for %s variants.", name, name).Line().Type().Id(name+"Type").String(), jen.Line())

		var consts []jen.Code
		for _, branch := range branches {
			value, desc := variantConst(defs, branch)
			if value == "" {
				continue
			}
			constName := fmt.Sprintf("%sType%s", name, pascalIdentifier(value))
			consts = append(consts, commented(constName, desc, jen.Id(constName).Id(name+"Type").Op("=").Lit(value)))
		}
		codes = append(codes, jen.Const().Defs(consts...), jen.Line())
	}

	inline := inlineUnion(branches)
	for _, branch := range branches {
		codes = append(codes, unionConstructorCode(defs, name, discriminator, branch, inline, fieldsByJSON), jen.Line())
	}
	if discriminator != "" && len(requiredSlices) > 0 {
		codes = append(codes, discriminatorUnionMarshalCode(name, discriminator, requiredSlices), jen.Line())
	}
	return codes
}

func discriminatorUnionMarshalCode(name, discriminator string, requiredSlices []unionRequiredSlice) jen.Code {
	receiver := receiverName(name)
	fieldNames := map[string]bool{}
	var wireFields []jen.Code
	for _, required := range requiredSlices {
		if required.discriminatorValue == "" || fieldNames[required.field.goName] {
			continue
		}
		fieldNames[required.field.goName] = true
		wireFields = append(wireFields, jen.Id(required.field.goName).Op("*").Add(required.field.typeCode).Tag(map[string]string{"json": jsonTag(required.field.jsonName, true, false)}))
	}

	var cases []jen.Code
	for _, required := range requiredSlices {
		if required.discriminatorValue == "" {
			continue
		}
		cases = append(cases, jen.Case(jen.Id(name+"Type"+pascalIdentifier(required.discriminatorValue))).Block(
			jen.Id(required.field.goName).Op(":=").Id(receiver).Dot(required.field.goName),
			jen.If(jen.Id(required.field.goName).Op("==").Nil()).Block(
				jen.Id(required.field.goName).Op("=").Add(required.field.typeCode).Values(),
			),
			jen.Id("w").Dot(required.field.goName).Op("=").Op("&").Id(required.field.goName),
		))
	}
	return jen.Comment("MarshalJSON implements json.Marshaler.").Line().Func().Params(jen.Id(receiver).Id(name)).Id("MarshalJSON").Params().Params(jen.Index().Byte(), jen.Error()).Block(
		jen.Type().Id("alias").Id(name),
		jen.Type().Id("wire").Struct(
			append([]jen.Code{jen.Op("*").Id("alias")}, wireFields...)...,
		),
		jen.Id("w").Op(":=").Id("wire").Values(jen.Id("alias").Op(":").Parens(jen.Op("*").Id("alias")).Call(jen.Op("&").Id(receiver))),
		jen.Switch(jen.Id(receiver).Dot(fieldName(discriminator))).Block(cases...),
		jen.Return(jen.Qual("encoding/json", "Marshal").Call(jen.Id("w"))),
	)
}

func unionConstructorCode(defs map[string]*jsonschema.Schema, unionName, discriminator string, branch *jsonschema.Schema, inline bool, fieldsByJSON map[string]unionField) jen.Code {
	constructorName := pascalIdentifier(branch.Title)
	value, _ := variantConst(defs, branch)
	ref := variantRef(branch)
	if constructorName == "" && value != "" {
		constructorName = pascalIdentifier(value)
	}
	if constructorName == "" && ref != "" {
		constructorName = strings.TrimPrefix(ref, unionName)
	}
	constructorName += unionName
	if ref != "" && constructorName == ref {
		constructorName = "New" + constructorName
	}

	properties := variantProperties(defs, branch)
	required := variantRequired(defs, branch)
	var params []jen.Code
	paramTypes := map[string]string{}
	for _, req := range required {
		prop := properties[req]
		if prop == nil || prop.Const != nil {
			continue
		}
		typ, text := schemaType(defs, prop, false)
		paramTypes[req] = text
		params = append(params, jen.Id(parameterName(req)).Add(typ))
	}

	var literalFields []jen.Code
	if value != "" && discriminator != "" {
		literalFields = append(literalFields, jen.Id(fieldName(discriminator)).Op(":").Id(unionName+"Type"+pascalIdentifier(value)))
	}
	for _, jsonName := range sortedPropertyNames(properties) {
		prop := properties[jsonName]
		if prop.Const != nil || !contains(required, jsonName) {
			continue
		}
		field := fieldsByJSON[jsonName]
		literalFields = append(literalFields, jen.Id(fieldName(jsonName)).Op(":").Add(assignUnionField(field.typeText, paramTypes[jsonName], parameterName(jsonName))))
	}

	fn := jen.Func().Id(constructorName).Params(params...).Id(unionName).Block(jen.Return(jen.Id(unionName).Values(multilineValues(literalFields)...)))
	if inline {
		description := strings.TrimSuffix(branch.Description, ".")
		return functionCommented(constructorName, "creates ", lowerFirstSentence(description)+".", fn)
	}
	return functionCommented(constructorName, fmt.Sprintf("creates an %s variant: ", unionName), branch.Description, fn)
}

func needsOmitZero(defs map[string]*jsonschema.Schema, schema *jsonschema.Schema) bool {
	if schema == nil {
		return false
	}
	if schema.Ref != "" {
		def := defs[refName(schema.Ref)]
		return isObjectSchema(def) || isDiscriminatorUnion(defs, def)
	}
	if len(schema.AllOf) == 1 && schema.AllOf[0].Ref != "" {
		return needsOmitZero(defs, schema.AllOf[0])
	}
	return isObjectSchema(schema) || isDiscriminatorUnion(defs, schema)
}

func schemaKind(defs map[string]*jsonschema.Schema, schema *jsonschema.Schema) string {
	if schema == nil {
		return ""
	}
	if schema.Ref != "" {
		return schemaKind(defs, defs[refName(schema.Ref)])
	}
	if len(schema.AllOf) == 1 && schema.AllOf[0].Ref != "" {
		return schemaKind(defs, defs[refName(schema.AllOf[0].Ref)])
	}
	return schemaTypeName(schema)
}

func assignUnionField(fieldType, paramType, paramName string) jen.Code {
	value := jen.Id(paramName)
	if fieldType == paramType || fieldType == "" || fieldType == "any" || paramType == "" {
		return value
	}
	if strings.HasPrefix(fieldType, "*") && strings.TrimPrefix(fieldType, "*") == paramType {
		return jen.Op("&").Id(paramName)
	}
	if strings.HasPrefix(fieldType, "*") {
		target := strings.TrimPrefix(fieldType, "*")
		return jen.Func().Params(jen.Id("v").Id(paramType)).Op("*").Id(target).Block(jen.Return(jen.Op("&").Id("v"))).Call(value)
	}
	return jen.Id(fieldType).Call(value)
}

func arrayUnionCode(defs map[string]*jsonschema.Schema, name string, schema *jsonschema.Schema) []jen.Code {
	branches := unionBranches(schema)
	var fields []jen.Code
	for _, branch := range branches {
		variantType := pascalIdentifier(branch.Title) + name
		fields = append(fields, jen.Id(arrayUnionFieldName(branch.Title)).Op("*").Id(variantType))
	}

	var codes []jen.Code
	codes = append(codes, commented(name, schema.Description, jen.Type().Id(name).Struct(fields...)), jen.Line())
	for _, branch := range branches {
		variantType := pascalIdentifier(branch.Title) + name
		typ, _ := schemaType(defs, branch, false)
		codes = append(codes,
			jen.Commentf("%s is the %s variant of %s.", variantType, strings.ToLower(branch.Title), name).Line().Type().Id(variantType).Add(typ),
			jen.Line(),
		)
	}
	codes = append(codes, arrayUnionMarshalCode(name, branches), jen.Line(), arrayUnionUnmarshalCode(defs, name, branches), jen.Line())
	return codes
}

func arrayUnionMarshalCode(name string, branches []*jsonschema.Schema) jen.Code {
	var body []jen.Code
	for i := len(branches) - 1; i >= 0; i-- {
		field := arrayUnionFieldName(branches[i].Title)
		body = append(body, jen.If(jen.Id("o").Dot(field).Op("!=").Nil()).Block(jen.Return(jen.Qual("encoding/json", "Marshal").Call(jen.Id("o").Dot(field)))))
	}
	body = append(body, jen.Return(jen.Index().Byte().Call(jen.Lit("null")), jen.Nil()))
	return jen.Comment("MarshalJSON implements json.Marshaler.").Line().Func().Params(jen.Id("o").Id(name)).Id("MarshalJSON").Params().Params(jen.Index().Byte(), jen.Error()).Block(body...)
}

func arrayUnionUnmarshalCode(defs map[string]*jsonschema.Schema, name string, branches []*jsonschema.Schema) jen.Code {
	flatType := pascalIdentifier(branches[0].Title) + name
	flatField := arrayUnionFieldName(branches[0].Title)
	groupBranch := branches[len(branches)-1]
	groupType := pascalIdentifier(groupBranch.Title) + name
	groupField := arrayUnionFieldName(groupBranch.Title)
	probeField := firstRequiredObjectField(defs[refName(groupBranch.Items.Ref)])

	return jen.Comment("UnmarshalJSON implements json.Unmarshaler.").Line().Func().Params(jen.Id("o").Op("*").Id(name)).Id("UnmarshalJSON").Params(jen.Id("data").Index().Byte()).Error().Block(
		jen.If(jen.String().Call(jen.Id("data")).Op("==").Lit("null")).Block(
			jen.Op("*").Id("o").Op("=").Id(name).Values(),
			jen.Return(jen.Nil()),
		),
		jen.Var().Id("raw").Index().Qual("encoding/json", "RawMessage"),
		jen.If(jen.Id("err").Op(":=").Qual("encoding/json", "Unmarshal").Call(jen.Id("data"), jen.Op("&").Id("raw")), jen.Id("err").Op("!=").Nil()).Block(jen.Return(jen.Id("err"))),
		jen.If(jen.Len(jen.Id("raw")).Op("==").Lit(0)).Block(
			jen.Id("flat").Op(":=").Id(flatType).Values(),
			jen.Op("*").Id("o").Op("=").Id(name).Values(jen.Id(flatField).Op(":").Op("&").Id("flat")),
			jen.Return(jen.Nil()),
		),
		jen.Var().Id("probe").Struct(jen.Id(fieldName(probeField)).String().Tag(map[string]string{"json": probeField})),
		jen.If(jen.Id("err").Op(":=").Qual("encoding/json", "Unmarshal").Call(jen.Id("raw").Index(jen.Lit(0)), jen.Op("&").Id("probe")), jen.Id("err").Op("!=").Nil()).Block(jen.Return(jen.Id("err"))),
		jen.If(jen.Id("probe").Dot(fieldName(probeField)).Op("!=").Lit("")).Block(
			jen.Var().Id("groups").Id(groupType),
			jen.If(jen.Id("err").Op(":=").Qual("encoding/json", "Unmarshal").Call(jen.Id("data"), jen.Op("&").Id("groups")), jen.Id("err").Op("!=").Nil()).Block(jen.Return(jen.Id("err"))),
			jen.Op("*").Id("o").Op("=").Id(name).Values(jen.Id(groupField).Op(":").Op("&").Id("groups")),
			jen.Return(jen.Nil()),
		),
		jen.Var().Id("flat").Id(flatType),
		jen.If(jen.Id("err").Op(":=").Qual("encoding/json", "Unmarshal").Call(jen.Id("data"), jen.Op("&").Id("flat")), jen.Id("err").Op("!=").Nil()).Block(jen.Return(jen.Id("err"))),
		jen.Op("*").Id("o").Op("=").Id(name).Values(jen.Id(flatField).Op(":").Op("&").Id("flat")),
		jen.Return(jen.Nil()),
	)
}

func isDiscriminatorUnion(defs map[string]*jsonschema.Schema, schema *jsonschema.Schema) bool {
	branches := unionBranches(schema)
	if len(branches) == 0 {
		return false
	}
	for _, branch := range branches {
		if branch.Title == "" && constValue(branch) == "" {
			return false
		}
		if len(variantProperties(defs, branch)) == 0 {
			return false
		}
	}
	return true
}

func isArrayUnion(schema *jsonschema.Schema) bool {
	branches := unionBranches(schema)
	if len(branches) != 2 {
		return false
	}
	for _, branch := range branches {
		if schemaTypeName(branch) != "array" || branch.Items == nil || branch.Items.Ref == "" || branch.Title == "" {
			return false
		}
	}
	return true
}

func unionBranches(schema *jsonschema.Schema) []*jsonschema.Schema {
	if len(schema.OneOf) > 0 {
		return schema.OneOf
	}
	return schema.AnyOf
}

func variantProperties(defs map[string]*jsonschema.Schema, branch *jsonschema.Schema) map[string]*jsonschema.Schema {
	properties := map[string]*jsonschema.Schema{}
	if ref := variantRef(branch); ref != "" {
		if def := defs[ref]; def != nil {
			for name, prop := range def.Properties {
				properties[name] = prop
			}
		}
	}
	for name, prop := range branch.Properties {
		properties[name] = prop
	}
	return properties
}

func variantRequired(defs map[string]*jsonschema.Schema, branch *jsonschema.Schema) []string {
	var required []string
	if ref := variantRef(branch); ref != "" {
		if def := defs[ref]; def != nil {
			required = append(required, def.Required...)
		}
	}
	for _, req := range branch.Required {
		if !contains(required, req) {
			required = append(required, req)
		}
	}
	return required
}

func variantConst(defs map[string]*jsonschema.Schema, branch *jsonschema.Schema) (string, string) {
	for _, prop := range variantProperties(defs, branch) {
		if text, ok := constString(prop); ok {
			return text, prop.Description
		}
	}
	return "", ""
}

func constValue(schema *jsonschema.Schema) string {
	if text, ok := constString(schema); ok {
		return text
	}
	for _, prop := range schema.Properties {
		if text, ok := constString(prop); ok {
			return text
		}
	}
	return ""
}

func variantRef(branch *jsonschema.Schema) string {
	if len(branch.AllOf) == 1 && branch.AllOf[0].Ref != "" {
		return refName(branch.AllOf[0].Ref)
	}
	return ""
}

func discriminatorField(defs map[string]*jsonschema.Schema, branches []*jsonschema.Schema) string {
	for _, branch := range branches {
		for jsonName, prop := range variantProperties(defs, branch) {
			if prop.Const != nil {
				return jsonName
			}
		}
	}
	return ""
}

func inlineUnion(branches []*jsonschema.Schema) bool {
	for _, branch := range branches {
		if variantRef(branch) != "" {
			return false
		}
	}
	return true
}

func multilineValues(fields []jen.Code) []jen.Code {
	if len(fields) == 0 {
		return nil
	}
	stmt := jen.Line()
	for _, field := range fields {
		stmt.Add(field).Op(",").Line()
	}
	return []jen.Code{stmt}
}

func arrayUnionFieldName(title string) string {
	if title == "Grouped" {
		return "Groups"
	}
	return pascalIdentifier(title)
}

func firstRequiredObjectField(schema *jsonschema.Schema) string {
	if schema != nil && len(schema.Required) > 0 {
		return schema.Required[0]
	}
	return "group"
}

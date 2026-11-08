package typegen

import (
	"github.com/dave/jennifer/jen"
	"github.com/google/jsonschema-go/jsonschema"
)

func fallbackUnionBranches(defs map[string]*jsonschema.Schema, branches []*jsonschema.Schema, discriminator string) []*jsonschema.Schema {
	var fallbacks []*jsonschema.Schema
	for _, branch := range branches {
		if _, constant := constString(variantProperties(defs, branch)[discriminator]); !constant {
			fallbacks = append(fallbacks, branch)
		}
	}
	return fallbacks
}

func fallbackExcludesTag(schema *jsonschema.Schema, discriminator, tag string) bool {
	if schema == nil || schema.Not == nil {
		return false
	}
	var excludes func(*jsonschema.Schema) bool
	excludes = func(s *jsonschema.Schema) bool {
		if s == nil {
			return false
		}
		if value, ok := constString(s.Properties[discriminator]); ok && value == tag && contains(s.Required, discriminator) {
			return true
		}
		for _, branch := range unionBranches(s) {
			if excludes(branch) {
				return true
			}
		}
		return false
	}
	return excludes(schema.Not)
}

func unionBranchProperties(defs map[string]*jsonschema.Schema, schema, branch *jsonschema.Schema) map[string]*jsonschema.Schema {
	return mergedProperties(schema.Properties, variantProperties(defs, branch))
}

func taggedBranchDecodeCode(defs map[string]*jsonschema.Schema, schema, branch *jsonschema.Schema, fields map[string]unionField, discriminator string, retainExtra, recordRequired bool) []jen.Code {
	properties := unionBranchProperties(defs, schema, branch)
	required := mergedRequired(schema.Required, variantRequired(defs, branch))
	body := propertiesValidationCode(defs, properties, required, jen.Id("data"))
	for _, jsonName := range sortedPropertyNames(properties) {
		if jsonName == discriminator || properties[jsonName].Const != nil {
			continue
		}
		field := fields[jsonName]
		source := newUnionField(defs, jsonName, properties[jsonName]).deserializeField
		source.goName = field.goName
		assign := func(value jen.Code) jen.Code {
			return jen.Id("decoded").Dot(field.goName).Op("=").Add(convertUnionFieldValue(field.typeText, source.typeText, value))
		}
		body = append(body, jen.If(jen.Len(jen.Id("raw").Dot(field.goName)).Op(">").Lit(0)).Block(
			preserveNullUnmarshalCode(source, deserializeVariantFieldCode(source, assign))...,
		))
	}
	if recordRequired {
		var names []jen.Code
		for _, name := range required {
			names = append(names, jen.Lit(name))
		}
		body = append(body, jen.Id("decoded").Dot("requiredFields").Op("=").Index().String().Values(names...))
	}
	if retainExtra && explicitExtraFields(branch) {
		body = append(body, captureExtraFieldsCode(properties, jen.Id("data"))...)
	}
	return append(body, jen.Return(jen.Nil()))
}

// Decode only the selected variant's typed fields. A flattened Go `any` field
// must not erase a branch's scalar type or make malformed known items valid.
// A genuine untagged fallback may be tried after a known branch fails; a custom
// branch with schema `not` exclusions must never hide a malformed known tag.
func taggedUnionUnmarshalCode(defs map[string]*jsonschema.Schema, name string, schema *jsonschema.Schema, fields map[string]unionField, branches []*jsonschema.Schema, discriminator, discriminatorGoName, discriminatorType string, retainExtra, recordRequired bool) jen.Code {
	receiver := receiverName(name)
	var rawFields []jen.Code
	rawFields = append(rawFields, jen.Id(discriminatorGoName).Qual("encoding/json", "RawMessage").Tag(map[string]string{"json": discriminator}))
	for _, jsonName := range sortedPropertyNamesFromUnion(fields) {
		rawFields = append(rawFields, jen.Id(fields[jsonName].goName).Qual("encoding/json", "RawMessage").Tag(map[string]string{"json": jsonName}))
	}
	body := []jen.Code{
		jen.Type().Id("alias").Id(name),
		jen.Var().Id("decoded").Id("alias"),
		jen.Var().Id("raw").Struct(rawFields...),
		jen.If(jen.Err().Op(":=").Id("unmarshalJSON").Call(jen.Id("data"), jen.Op("&").Id("raw")), jen.Err().Op("!=").Nil()).Block(jen.Return(jen.Err())),
	}
	fallbacks := fallbackUnionBranches(defs, branches, discriminator)
	body = append(body,
		jen.Var().Id("tag").Id(discriminatorType),
		jen.Var().Id("tagErr").Error(),
		jen.If(jen.Len(jen.Id("raw").Dot(discriminatorGoName)).Op(">").Lit(0)).Block(
			jen.Id("tagErr").Op("=").Id("unmarshalJSON").Call(jen.Id("raw").Dot(discriminatorGoName), jen.Op("&").Id("tag")),
		),
	)
	candidate := func(branch *jsonschema.Schema) jen.Code {
		prefix := []jen.Code{jen.Id("decoded").Op("=").Id("alias").Values()}
		if unionBranchProperties(defs, schema, branch)[discriminator] != nil {
			prefix = append(prefix,
				jen.If(jen.Id("tagErr").Op("!=").Nil()).Block(jen.Return(jen.Id("tagErr"))),
				jen.Id("decoded").Dot(discriminatorGoName).Op("=").Id("tag"),
			)
		}
		prefix = append(prefix, taggedBranchDecodeCode(defs, schema, branch, fields, discriminator, retainExtra, recordRequired)...)
		return jen.Func().Params().Error().Block(prefix...).Call()
	}
	try := func(candidates []*jsonschema.Schema) []jen.Code {
		var code []jen.Code
		for _, branch := range candidates {
			for _, alternative := range selectedObjectBranches(defs, schema, branch) {
				assignment := jen.Err().Op("=").Add(candidate(alternative))
				if len(code) == 0 {
					code = append(code, assignment)
				} else {
					code = append(code, jen.If(jen.Err().Op("!=").Nil()).Block(assignment))
				}
			}
		}
		return code
	}
	var cases []jen.Code
	for _, branch := range branches {
		value, constant := constString(variantProperties(defs, branch)[discriminator])
		if !constant {
			continue
		}
		candidates := []*jsonschema.Schema{branch}
		for _, fallback := range fallbacks {
			if !fallbackExcludesTag(fallback, discriminator, value) {
				candidates = append(candidates, fallback)
			}
		}
		cases = append(cases, jen.Case(jen.Lit(value)).Block(try(candidates)...))
	}
	if len(fallbacks) > 0 {
		cases = append(cases, jen.Default().Block(try(fallbacks)...))
	} else {
		cases = append(cases, jen.Default().Block(jen.Return(jen.Qual("fmt", "Errorf").Call(jen.Lit(name+": unknown "+discriminator+" %q"), jen.Id("tag")))))
	}
	body = append(body,
		jen.Var().Err().Error(),
		jen.Switch(jen.Id("tag")).Block(cases...),
		jen.If(jen.Err().Op("!=").Nil()).Block(jen.Return(jen.Err())),
		jen.Op("*").Id(receiver).Op("=").Id(name).Call(jen.Id("decoded")),
		jen.Return(jen.Nil()),
	)
	return jen.Comment("UnmarshalJSON decodes the selected schema variant.").Line().Func().Params(jen.Id(receiver).Op("*").Id(name)).Id("UnmarshalJSON").Params(jen.Id("data").Index().Byte()).Error().Block(body...)
}

func sortedPropertyNamesFromUnion(fields map[string]unionField) []string {
	properties := make(map[string]*jsonschema.Schema, len(fields))
	for name := range fields {
		properties[name] = nil
	}
	return sortedPropertyNames(properties)
}

func unionExtraMarshalReturn(defs map[string]*jsonschema.Schema, schema *jsonschema.Schema, discriminator, discriminatorGoName string, receiver string, value jen.Code) jen.Code {
	var cases []jen.Code
	var fallback jen.Code
	for _, branch := range unionBranches(schema) {
		properties := unionBranchProperties(defs, schema, branch)
		var code jen.Code = jen.Return(jen.Qual("encoding/json", "Marshal").Call(value))
		if explicitExtraFields(branch) {
			code = jen.Return(jen.Id("marshalJSONWithExtraFields").Call(value, jen.Id(receiver).Dot("ExtraFields"), jsonFieldNames(properties)))
		}
		if tag, constant := constString(properties[discriminator]); constant {
			cases = append(cases, jen.Case(jen.Lit(tag)).Block(code))
		} else if fallback == nil || explicitExtraFields(branch) {
			// Prefer the explicit open-variant retention path over an untagged
			// fallback. Untagged decoded values have no captured extra fields.
			fallback = code
		}
	}
	if fallback == nil {
		fallback = jen.Return(jen.Qual("encoding/json", "Marshal").Call(value))
	}
	cases = append(cases, jen.Default().Block(fallback))
	return jen.Switch(jen.Id(receiver).Dot(discriminatorGoName)).Block(cases...)
}

func simpleExtraUnionMarshalCode(defs map[string]*jsonschema.Schema, name string, schema *jsonschema.Schema, discriminator, discriminatorGoName string) jen.Code {
	receiver := receiverName(name)
	return jen.Comment("MarshalJSON retains explicit extension fields for open variants.").Line().Func().Params(jen.Id(receiver).Id(name)).Id("MarshalJSON").Params().Params(jen.Index().Byte(), jen.Error()).Block(
		jen.Type().Id("alias").Id(name),
		unionExtraMarshalReturn(defs, schema, discriminator, discriminatorGoName, receiver, jen.Id("alias").Call(jen.Id(receiver))),
	)
}

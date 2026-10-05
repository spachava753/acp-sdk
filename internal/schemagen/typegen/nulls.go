package typegen

import (
	"github.com/dave/jennifer/jen"
	"github.com/google/jsonschema-go/jsonschema"
)

// These upstream fields distinguish null from omission in prose, but do not
// expose a schema keyword for that distinction. Keep those semantic overrides
// here rather than changing the downloaded schema or all optional field types.
var nullSensitiveFields = map[string][]string{
	"SubagentUpdate": {"title", "description", "capabilities", "state", "_meta"},
	"SessionMessage": {"content", "_meta"},
	"McpError":       {"data"},
}

func preservesJSONNull(defs map[string]*jsonschema.Schema, jsonName string, prop *jsonschema.Schema) bool {
	if schemaBoolExtra(prop, "x-go-preserve-null") {
		return true
	}
	for name, fields := range nullSensitiveFields {
		if def := defs[name]; def != nil && def.Properties[jsonName] == prop && contains(fields, jsonName) {
			return true
		}
	}
	return false
}

func nullFieldsDeclaration() jen.Code {
	return jen.Comment("NullFields lists JSON field names to send as explicit null, regardless of their values.").Line().
		Comment("Decoding records explicit nulls here, separately from omitted fields.").Line().
		Id("NullFields").Index().String().Tag(map[string]string{"json": "-"})
}

func preservedNullFields(fields []deserializeField) []deserializeField {
	var result []deserializeField
	for _, field := range fields {
		if field.preserveNull {
			result = append(result, field)
		}
	}
	return result
}

func preserveNullUnmarshalCode(field deserializeField, body []jen.Code) []jen.Code {
	if !field.preserveNull {
		return body
	}
	return []jen.Code{
		jen.If(jen.Id("isJSONNull").Call(jen.Id("raw").Dot(field.goName))).Block(
			jen.Id("decoded").Dot("NullFields").Op("=").Append(jen.Id("decoded").Dot("NullFields"), jen.Lit(field.jsonName)),
		).Else().Block(body...),
	}
}

func nullFieldMarshalAssignments(receiver string, fields []deserializeField) []jen.Code {
	var body []jen.Code
	for _, field := range fields {
		body = append(body, jen.If(jen.Qual("slices", "Contains").Call(jen.Id(receiver).Dot("NullFields"), jen.Lit(field.jsonName))).Block(
			jen.Id("w").Dot(field.goName).Op("=").New(field.typeCode),
		))
	}
	return body
}

func nullFieldWireFields(fields []deserializeField) []jen.Code {
	var result []jen.Code
	for _, field := range fields {
		result = append(result, jen.Id(field.goName).Op("*").Add(field.typeCode).Tag(map[string]string{"json": jsonTag(field.jsonName, true, false)}))
	}
	return result
}

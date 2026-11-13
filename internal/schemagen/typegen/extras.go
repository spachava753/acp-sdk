package typegen

import (
	"github.com/dave/jennifer/jen"
	"github.com/google/jsonschema-go/jsonschema"
)

func explicitExtraFields(schema *jsonschema.Schema) bool {
	if schema == nil {
		return false
	}
	for _, extra := range []*jsonschema.Schema{schema.AdditionalProperties, schema.UnevaluatedProperties} {
		if extra != nil && extra.Not == nil && extra.Ref == "" && schemaTypeName(extra) == "" && len(extra.Properties) == 0 && len(unionBranches(extra)) == 0 {
			return true
		}
	}
	return false
}

func extraFieldsDeclaration() jen.Code {
	return jen.Comment("ExtraFields retains extension properties explicitly permitted by the schema.").Line().
		Comment("Known properties take precedence when encoding.").Line().
		Id("ExtraFields").Map(jen.String()).Qual("encoding/json", "RawMessage").Tag(map[string]string{"json": "-"})
}

func jsonFieldNames(properties map[string]*jsonschema.Schema) jen.Code {
	var names []jen.Code
	for _, name := range sortedPropertyNames(properties) {
		names = append(names, jen.Lit(name))
	}
	return jen.Index().String().Values(names...)
}

func captureExtraFieldsCode(properties map[string]*jsonschema.Schema, data jen.Code) []jen.Code {
	return []jen.Code{
		jen.List(jen.Id("extra"), jen.Err()).Op(":=").Id("captureJSONExtraFields").Call(data, jsonFieldNames(properties)),
		jen.If(jen.Err().Op("!=").Nil()).Block(jen.Return(jen.Err())),
		jen.Id("decoded").Dot("ExtraFields").Op("=").Id("extra"),
	}
}

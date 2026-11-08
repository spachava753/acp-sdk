package typegen

import (
	"github.com/dave/jennifer/jen"
	"github.com/google/jsonschema-go/jsonschema"
)

// Untagged object alternatives select the first fully valid candidate, not the
// intersection of its required keys or the first merely present carrier.
func untaggedUnionUnmarshalCode(defs map[string]*jsonschema.Schema, name string, schema *jsonschema.Schema, fields map[string]unionField, retainExtra bool) jen.Code {
	receiver := receiverName(name)
	var rawFields []jen.Code
	for _, jsonName := range sortedPropertyNamesFromUnion(fields) {
		rawFields = append(rawFields, jen.Id(fields[jsonName].goName).Qual("encoding/json", "RawMessage").Tag(map[string]string{"json": jsonName}))
	}
	body := []jen.Code{
		jen.Type().Id("alias").Id(name),
		jen.Var().Id("decoded").Id("alias"),
		jen.Var().Id("raw").Struct(rawFields...),
		jen.If(jen.Err().Op(":=").Id("unmarshalJSON").Call(jen.Id("data"), jen.Op("&").Id("raw")), jen.Err().Op("!=").Nil()).Block(jen.Return(jen.Err())),
		jen.Var().Err().Error(),
	}
	for _, alternative := range objectBranches(defs, schema) {
		decode := []jen.Code{jen.Id("decoded").Op("=").Id("alias").Values()}
		decode = append(decode, taggedBranchDecodeCode(defs, schema, alternative, fields, "", retainExtra, false)...)
		body = append(body,
			jen.Err().Op("=").Func().Params().Error().Block(decode...).Call(),
			jen.If(jen.Err().Op("==").Nil()).Block(
				jen.Op("*").Id(receiver).Op("=").Id(name).Call(jen.Id("decoded")),
				jen.Return(jen.Nil()),
			),
		)
	}
	body = append(body, jen.Return(jen.Err()))
	return jen.Comment("UnmarshalJSON decodes the first valid schema alternative.").Line().Func().Params(jen.Id(receiver).Op("*").Id(name)).Id("UnmarshalJSON").Params(jen.Id("data").Index().Byte()).Error().Block(body...)
}

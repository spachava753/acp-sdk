package typegen

import "github.com/dave/jennifer/jen"

// This is called only for genuinely closed string enums. Primitive unions
// containing an unconstrained string branch remain open to future values.
func closedStringUnmarshalCode(name string, values []string) jen.Code {
	var cases []jen.Code
	for _, value := range values {
		cases = append(cases, jen.Lit(value))
	}
	return jen.Comment("UnmarshalJSON rejects values outside the closed schema enum.").Line().
		Func().Params(jen.Id("v").Op("*").Id(name)).Id("UnmarshalJSON").Params(jen.Id("data").Index().Byte()).Error().Block(
		jen.Var().Id("value").String(),
		jen.If(jen.Id("isJSONNull").Call(jen.Id("data"))).Block(jen.Return(jen.Qual("fmt", "Errorf").Call(jen.Lit(name+" must not be null")))),
		jen.If(jen.Err().Op(":=").Id("unmarshalJSON").Call(jen.Id("data"), jen.Op("&").Id("value")), jen.Err().Op("!=").Nil()).Block(jen.Return(jen.Err())),
		jen.Switch(jen.Id("value")).Block(jen.Case(cases...).Block(
			jen.Op("*").Id("v").Op("=").Id(name).Call(jen.Id("value")),
			jen.Return(jen.Nil()),
		)),
		jen.Return(jen.Qual("fmt", "Errorf").Call(jen.Lit("invalid "+name+" value %q"), jen.Id("value"))),
	)
}

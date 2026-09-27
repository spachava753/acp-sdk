package typegen

import (
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/google/jsonschema-go/jsonschema"
)

func unstructuredType(defs map[string]*jsonschema.Schema, text string) bool {
	return numberType(defs, text, true, map[string]bool{})
}

func requiresContainerDecoder(defs map[string]*jsonschema.Schema, text string) bool {
	return numberType(defs, text, false, map[string]bool{})
}

func numberType(defs map[string]*jsonschema.Schema, text string, collections bool, seen map[string]bool) bool {
	if collections {
		for {
			previous := text
			text = strings.TrimPrefix(text, "*")
			text = strings.TrimPrefix(text, "[]")
			text = strings.TrimPrefix(text, "map[string]")
			if previous == text {
				break
			}
		}
	}
	if text == "any" || (!collections && strings.HasPrefix(text, "*")) {
		return true
	}
	if text == "" || text[0] < 'A' || text[0] > 'Z' || seen[text] {
		return false
	}
	seen[text] = true
	for name, goName := range goDefinitionNames(defs) {
		if goName != text {
			continue
		}
		def := defs[name]
		if isObjectSchema(def) || isDiscriminatorUnion(defs, def) || isArrayUnion(def) {
			return false
		}
		_, underlying := schemaType(defs, def, false)
		return numberType(defs, underlying, collections, seen)
	}
	return false
}

func numberUnmarshalCode(name string) jen.Code {
	receiver := receiverName(name)
	return jen.Comment("UnmarshalJSON preserves unstructured numbers as json.Number.").Line().
		Func().Params(jen.Id(receiver).Op("*").Id(name)).Id("UnmarshalJSON").Params(jen.Id("data").Index().Byte()).Error().Block(
		jen.Type().Id("plain").Id(name),
		jen.Return(jen.Id("unmarshalJSON").Call(jen.Id("data"), jen.Parens(jen.Op("*").Id("plain")).Call(jen.Id(receiver)))),
	)
}

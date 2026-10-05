package typegen

import (
	"sort"
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/google/jsonschema-go/jsonschema"
)

// A union of object carriers can select its branch by a distinct required key.
// Schema order gives the receiver's precedence when more than one key is present.
func hasRequiredKeyBranches(defs map[string]*jsonschema.Schema, schema *jsonschema.Schema) bool {
	if len(schema.AnyOf) < 2 || len(schema.Required) != 0 || len(schema.Properties) != 0 {
		return false
	}
	seen := map[string]bool{}
	for _, branch := range schema.AnyOf {
		required := variantRequired(defs, branch)
		if len(required) != 1 || seen[required[0]] {
			return false
		}
		prop := variantProperties(defs, branch)[required[0]]
		if prop == nil || prop.Const != nil {
			return false
		}
		// Existing pointer fields cannot represent presence of a nullable typed
		// carrier. Unconstrained any carriers use a separate presence pointer.
		if _, nullable := nullableSchema(prop); nullable {
			return false
		}
		seen[required[0]] = true
	}
	return true
}

func requiredKeyUnionMarshalCode(defs map[string]*jsonschema.Schema, name string, schema *jsonschema.Schema, fields map[string]unionField) jen.Code {
	receiver := receiverName(name)
	var body []jen.Code
	var keys []string
	for _, branch := range schema.AnyOf {
		key := variantRequired(defs, branch)[0]
		keys = append(keys, key)
		carrier := fields[key]
		var wireFields, values []jen.Code
		for _, jsonName := range sortedPropertyNames(variantProperties(defs, branch)) {
			field := fields[jsonName]
			wireFields = append(wireFields, jen.Id(field.goName).Add(field.typeCode).Tag(map[string]string{"json": jsonTag(jsonName, jsonName != key && jsonName != "_meta", jsonName == "_meta")}))
			values = append(values, jen.Id(receiver).Dot(field.goName))
		}
		body = append(body, jen.If(jen.Id(receiver).Dot(carrier.goName).Op("!=").Nil()).Block(
			jen.Return(jen.Qual("encoding/json", "Marshal").Call(jen.Struct(wireFields...).Values(values...))),
		))
	}
	body = append(body, jen.Return(jen.Nil(), jen.Qual("fmt", "Errorf").Call(jen.Lit(name+" requires one of: "+strings.Join(keys, ", ")))))
	return jen.Comment("MarshalJSON emits the first present carrier in schema order.").Line().Func().Params(jen.Id(receiver).Id(name)).Id("MarshalJSON").Params().Params(jen.Index().Byte(), jen.Error()).Block(body...)
}

func requiredKeyUnionUnmarshalCode(defs map[string]*jsonschema.Schema, name string, schema *jsonschema.Schema, fields map[string]unionField) jen.Code {
	receiver := receiverName(name)
	var rawFields []jen.Code
	var names []string
	for jsonName := range fields {
		names = append(names, jsonName)
	}
	sort.Strings(names)
	for _, jsonName := range names {
		rawFields = append(rawFields, jen.Id(fields[jsonName].goName).Qual("encoding/json", "RawMessage").Tag(map[string]string{"json": jsonName}))
	}
	body := []jen.Code{
		jen.Type().Id("alias").Id(name),
		jen.Var().Id("decoded").Id("alias"),
		jen.Var().Id("raw").Struct(rawFields...),
		jen.If(jen.Err().Op(":=").Id("unmarshalJSON").Call(jen.Id("data"), jen.Op("&").Id("raw")), jen.Err().Op("!=").Nil()).Block(jen.Return(jen.Err())),
	}
	var cases []jen.Code
	var keys []string
	for _, branch := range schema.AnyOf {
		key := variantRequired(defs, branch)[0]
		keys = append(keys, key)
		properties := variantProperties(defs, branch)
		carrier := fields[key]
		var decode []jen.Code
		if !allowsJSONNull(defs, properties[key]) {
			decode = append(decode, jen.If(jen.Id("isJSONNull").Call(jen.Id("raw").Dot(carrier.goName))).Block(
				jen.Return(jen.Qual("fmt", "Errorf").Call(jen.Lit(name+"."+key+" must not be null"))),
			))
		}
		if object := resolvedObjectSchema(defs, properties[key]); object != nil {
			var required, nonNull []jen.Code
			for _, propName := range object.Required {
				required = append(required, jen.Lit(propName))
				if !allowsJSONNull(defs, object.Properties[propName]) {
					nonNull = append(nonNull, jen.Lit(propName))
				}
			}
			decode = append(decode, jen.If(jen.Err().Op(":=").Id("requireJSONProperties").Call(jen.Id("raw").Dot(carrier.goName), jen.Index().String().Values(required...), jen.Index().String().Values(nonNull...)), jen.Err().Op("!=").Nil()).Block(
				jen.Return(jen.Qual("fmt", "Errorf").Call(jen.Lit(name+"."+key+": %w"), jen.Err())),
			))
		}
		for _, jsonName := range sortedPropertyNames(properties) {
			source := newUnionField(defs, jsonName, properties[jsonName]).deserializeField
			source.goName = fields[jsonName].goName
			// Unlike flattened tagged unions, each carrier decodes only its own
			// fields, including the schema's tolerant metadata annotation.
			source.defaultOnError = schemaBoolExtra(properties[jsonName], "x-deserialize-default-on-error")
			assign := func(value jen.Code) jen.Code {
				converted := convertUnionFieldValue(fields[jsonName].typeText, source.typeText, value)
				if fields[jsonName].typeText == "*"+source.typeText {
					converted = jen.Op("&").Add(value)
				}
				return jen.Id("decoded").Dot(source.goName).Op("=").Add(converted)
			}
			code := preserveNullUnmarshalCode(source, deserializeVariantFieldCode(source, assign))
			if jsonName == key {
				decode = append(decode, code...)
			} else {
				decode = append(decode, jen.If(jen.Len(jen.Id("raw").Dot(source.goName)).Op(">").Lit(0)).Block(code...))
			}
		}
		cases = append(cases, jen.Case(jen.Len(jen.Id("raw").Dot(carrier.goName)).Op(">").Lit(0)).Block(decode...))
	}
	cases = append(cases, jen.Default().Block(jen.Return(jen.Qual("fmt", "Errorf").Call(jen.Lit(name+" requires one of: "+strings.Join(keys, ", "))))))
	body = append(body,
		jen.Switch().Block(cases...),
		jen.Op("*").Id(receiver).Op("=").Id(name).Call(jen.Id("decoded")),
		jen.Return(jen.Nil()),
	)
	return jen.Comment("UnmarshalJSON selects the first present carrier in schema order.").Line().Func().Params(jen.Id(receiver).Op("*").Id(name)).Id("UnmarshalJSON").Params(jen.Id("data").Index().Byte()).Error().Block(body...)
}

func allowsJSONNull(defs map[string]*jsonschema.Schema, schema *jsonschema.Schema) bool {
	if schema == nil {
		return true
	}
	if schema.Ref != "" {
		return allowsJSONNull(defs, defs[refName(schema.Ref)])
	}
	if len(schema.AllOf) == 1 {
		return allowsJSONNull(defs, schema.AllOf[0])
	}
	if _, nullable := nullableSchema(schema); nullable {
		return true
	}
	return schemaTypeName(schema) == "null" || (schemaTypeName(schema) == "" && len(unionBranches(schema)) == 0)
}

func resolvedObjectSchema(defs map[string]*jsonschema.Schema, schema *jsonschema.Schema) *jsonschema.Schema {
	if schema == nil {
		return nil
	}
	if schema.Ref != "" {
		return resolvedObjectSchema(defs, defs[refName(schema.Ref)])
	}
	if len(schema.AllOf) == 1 {
		return resolvedObjectSchema(defs, schema.AllOf[0])
	}
	if isObjectSchema(schema) {
		return schema
	}
	return nil
}

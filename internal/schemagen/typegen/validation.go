package typegen

import (
	"github.com/dave/jennifer/jen"
	"github.com/google/jsonschema-go/jsonschema"
)

// Required presence is independent of valid zero values. Null is a field error
// unless the schema permits it or explicitly declares recovery/defaulting.
func propertiesValidationCode(defs map[string]*jsonschema.Schema, properties map[string]*jsonschema.Schema, required []string, data jen.Code) []jen.Code {
	var keys, nonNull []jen.Code
	for _, name := range required {
		keys = append(keys, jen.Lit(name))
	}
	for _, name := range sortedPropertyNames(properties) {
		prop := properties[name]
		if !allowsJSONNull(defs, prop) && !schemaBoolExtra(prop, "x-deserialize-default-on-error") && !schemaBoolExtra(prop, "x-deserialize-default-on-null") {
			nonNull = append(nonNull, jen.Lit(name))
		}
	}
	if len(keys) == 0 && len(nonNull) == 0 {
		return nil
	}
	return []jen.Code{jen.If(jen.Err().Op(":=").Id("requireJSONProperties").Call(data, jen.Index().String().Values(keys...), jen.Index().String().Values(nonNull...)), jen.Err().Op("!=").Nil()).Block(jen.Return(jen.Err()))}
}

func resolveValidationSchema(defs map[string]*jsonschema.Schema, schema *jsonschema.Schema) *jsonschema.Schema {
	for schema != nil {
		if schema.Ref != "" {
			schema = defs[refName(schema.Ref)]
		} else if len(schema.AllOf) == 1 {
			schema = schema.AllOf[0]
		} else {
			return schema
		}
	}
	return nil
}

// RawMessage aliases remain aliases, so direct callers retain that API. Typed
// containing fields validate the simple raw unions their schemas constrain.
func fieldRawValidator(defs map[string]*jsonschema.Schema, schema *jsonschema.Schema) jen.Code {
	schema = resolveValidationSchema(defs, schema)
	if schema == nil {
		return nil
	}
	if nonNull, nullable := nullableSchema(schema); nullable {
		child := fieldRawValidator(defs, nonNull)
		if child == nil {
			return nil
		}
		return jen.Func().Params(jen.Id("data").Index().Byte()).Error().Block(
			jen.If(jen.Id("isJSONNull").Call(jen.Id("data"))).Block(jen.Return(jen.Nil())),
			jen.Return(jen.Add(child).Call(jen.Id("data"))),
		)
	}
	if schemaTypeName(schema) == "object" && len(schema.Properties) == 0 && schema.AdditionalProperties != nil {
		if child := fieldRawValidator(defs, schema.AdditionalProperties); child != nil {
			return collectionValidator("validateJSONMapValues", child)
		}
	}
	if schemaTypeName(schema) == "array" && schema.Items != nil {
		if child := fieldRawValidator(defs, schema.Items); child != nil {
			return collectionValidator("validateJSONArrayValues", child)
		}
	}
	if len(unionBranches(schema)) < 2 || isDiscriminatorUnion(defs, schema) || isArrayUnion(schema) || isStringConstEnum(schema) || isPrimitiveConstUnion(schema) {
		return nil
	}
	return jsonUnionValidator(defs, unionBranches(schema))
}

func jsonUnionValidator(defs map[string]*jsonschema.Schema, branches []*jsonschema.Schema) jen.Code {
	var validators []jen.Code
	for _, branch := range branches {
		validator := jsonKindValidator(defs, branch)
		if validator == nil {
			return nil // Complex unions keep their existing raw representation.
		}
		validators = append(validators, validator)
	}
	return jen.Func().Params(jen.Id("data").Index().Byte()).Error().Block(
		jen.Return(jen.Id("validateJSONUnion").Call(append([]jen.Code{jen.Id("data")}, validators...)...)),
	)
}

func collectionValidator(helper string, child jen.Code) jen.Code {
	return jen.Func().Params(jen.Id("data").Index().Byte()).Error().Block(jen.Return(jen.Id(helper).Call(jen.Id("data"), child)))
}

func jsonKindValidator(defs map[string]*jsonschema.Schema, schema *jsonschema.Schema) jen.Code {
	schema = resolveValidationSchema(defs, schema)
	if schema == nil || schema.Const != nil || len(schema.Enum) != 0 {
		return nil
	}
	if branches := unionBranches(schema); len(branches) > 0 {
		return jsonUnionValidator(defs, branches)
	}
	if len(schema.Types) > 1 {
		var branches []*jsonschema.Schema
		for _, typ := range schema.Types {
			branch := *schema
			branch.Type, branch.Types = typ, nil
			branches = append(branches, &branch)
		}
		return jsonUnionValidator(defs, branches)
	}
	var typ jen.Code
	switch schemaTypeName(schema) {
	case "null":
		return jen.Id("validateJSONNull")
	case "string":
		typ = jen.String()
	case "integer":
		typ, _ = integerType(schema.Format)
	case "number":
		if schema.Format == "" {
			return jen.Id("validateJSONNumber")
		}
		typ, _ = schemaType(defs, schema, false)
	case "boolean":
		typ = jen.Bool()
	case "array":
		if item := jsonKindValidator(defs, schema.Items); item != nil {
			return collectionValidator("validateJSONArrayValues", item)
		}
		return nil
	default:
		return nil
	}
	return jen.Id("validateJSONType").Types(typ)
}

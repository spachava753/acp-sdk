package typegen

import (
	"testing"

	"github.com/dave/jennifer/jen"
	"github.com/google/jsonschema-go/jsonschema"
)

func TestSchemaTypePanicsOnUnsupportedRef(t *testing.T) {
	assertPanics(t, func() {
		schemaType(nil, &jsonschema.Schema{Ref: "https://example.com/schema.json"}, false)
	})

	assertPanics(t, func() {
		schemaType(map[string]*jsonschema.Schema{}, &jsonschema.Schema{Ref: "#/$defs/Missing"}, false)
	})
}

func TestSchemaTypePanicsOnUnsupportedPrimitive(t *testing.T) {
	assertPanics(t, func() {
		schemaType(nil, &jsonschema.Schema{Type: "unknown"}, false)
	})

	assertPanics(t, func() {
		schemaType(nil, &jsonschema.Schema{Type: "integer", Format: "uint8"}, false)
	})

	assertPanics(t, func() {
		schemaType(nil, &jsonschema.Schema{Type: "number", Format: "float"}, false)
	})
}

func TestNewDeserializeFieldPanicsOnMalformedSkipInvalidItems(t *testing.T) {
	assertPanics(t, func() {
		newDeserializeField(nil, "value", &jsonschema.Schema{
			Type:  "string",
			Extra: map[string]any{"x-deserialize-skip-invalid-items": true},
		}, jen.String(), "string")
	})

	assertPanics(t, func() {
		newDeserializeField(nil, "values", &jsonschema.Schema{
			Type:  "array",
			Extra: map[string]any{"x-deserialize-skip-invalid-items": true},
		}, jen.Index().String(), "[]string")
	})
}

func assertPanics(t *testing.T, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	f()
}

package schemagen

import (
	"github.com/google/jsonschema-go/jsonschema"
)

type GeneratedFile struct {
	Filename string
	Contents []byte
}

// Generate takes a json schema and returns a client file contents and agent file contente
func Generate(schema *jsonschema.Schema) []GeneratedFile {
	panic("unimplemented")
}

package schemagen

import (
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spachava753/acp-sdk/internal/schemagen/rpcgen"
	"github.com/spachava753/acp-sdk/internal/schemagen/typegen"
)

type GeneratedFile struct {
	Filename string
	Contents []byte
}

// Generate returns the ACP source files for schema.
func Generate(schema *jsonschema.Schema) []GeneratedFile {
	var gf []GeneratedFile
	if contents := typegen.Generate(schema); len(contents) > 0 {
		gf = append(gf, GeneratedFile{Filename: "types_gen.go", Contents: contents})
	}
	for _, file := range rpcgen.Generate(schema) {
		gf = append(gf, GeneratedFile{Filename: file.Name, Contents: file.Contents})
	}
	return gf
}

package schemagen

import (
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spachava753/acp-sdk/internal/schemagen/agentgen"
	"github.com/spachava753/acp-sdk/internal/schemagen/typegen"
)

type GeneratedFile struct {
	Filename string
	Contents []byte
}

// Generate takes a json schema and returns generated ACP source files.
func Generate(schema *jsonschema.Schema) []GeneratedFile {
	// The eventual generator has three passes: types, agent RPC glue, and client
	// RPC glue. This first pass intentionally emits only protocol data types so
	// the type-shape rules can be reviewed independently before wiring methods.
	var gf []GeneratedFile
	if contents := typegen.Generate(schema); len(contents) > 0 {
		gf = append(gf, GeneratedFile{Filename: "types_gen.go", Contents: contents})
	}
	if contents := agentgen.Generate(schema); len(contents) > 0 {
		gf = append(gf, GeneratedFile{Filename: "agent_gen.go", Contents: contents})
	}
	return gf
}

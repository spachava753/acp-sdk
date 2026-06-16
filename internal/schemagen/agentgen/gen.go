// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package agentgen

import (
	"bytes"

	"github.com/dave/jennifer/jen"
	"github.com/google/jsonschema-go/jsonschema"
)

type operation struct {
	method       string
	side         string
	group        string
	constName    string
	funcName     string
	requestType  string
	responseType string
	notifyType   string
	description  string
}

// Generate returns the contents of agent_gen.go for schema.
func Generate(schema *jsonschema.Schema) []byte {
	if schema == nil || len(schema.Defs) == 0 {
		return nil
	}
	ops := operations(schema)
	if len(ops) == 0 {
		return nil
	}

	file := jen.NewFile("acp")
	emitMethodConsts(file, ops)
	emitAgentHandlers(file, ops)
	emitAgentConnectionMethods(file, ops)
	emitHandleAgentRequest(file, ops)

	var out bytes.Buffer
	if err := file.Render(&out); err != nil {
		return nil
	}
	return out.Bytes()
}
